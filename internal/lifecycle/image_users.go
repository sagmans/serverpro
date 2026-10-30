package lifecycle

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	// ImageUsersArchiveDir keeps what removal takes away (the home, crontab,
	// and sudoers files) root-only on the host, so an operator who finds a
	// workload that needed the account can restore it.
	ImageUsersArchiveDir = "/root/serverpro-removed-accounts"
	// imageUserRemovalFailedPrefix starts every reason the removal stops, so
	// operators and tests can find it in output.
	imageUserRemovalFailedPrefix = "image account removal stopped: "
	// sudoersMainPath and sudoersDropInDir are the sudoers files the removal
	// knows: it edits only files directly in the drop-in directory, and it
	// requires the main file in visudo's list so an empty list cannot read
	// as "nothing names it".
	sudoersMainPath   = "/etc/sudoers"
	sudoersDropInDir  = "/etc/sudoers.d"
	sudoersDropInGlob = sudoersDropInDir + "/*"
	// SudoersParserPath is sudo's own sudoers parser, shipped in Ubuntu's
	// sudo package. Doctor and the image account removal read the rules
	// through it instead of line patterns, because sudo accepts forms a line
	// pattern misreads: continued lines, user lists, #uid principals,
	// aliases, and includes. The absolute path keeps a PATH entry from
	// standing in for it.
	SudoersParserPath = "/usr/bin/cvtsudoers"
	// SudoersPolicyCommand prints every rule and Defaults entry sudo reads,
	// starting from the main file and following each include, as JSON with
	// aliases expanded. The empty config keeps a cvtsudoers.conf filter from
	// hiding rules.
	SudoersPolicyCommand = SudoersParserPath + " -c /dev/null -e -f json " + sudoersMainPath
	// sudoersPolicyShape is a line cvtsudoers' JSON holds whenever it lists
	// a rule, and every serverpro host has the admin's rule. Without it the
	// removal cannot trust that the parse below says what it expects, so
	// it stops before userdel.
	sudoersPolicyShape = `"User_Specs": [`
	// sudoersUserNameKey and sudoersUserIDKey are the JSON keys cvtsudoers
	// writes for an account named by name or by #uid, in a rule's user or
	// run-as list or in a Defaults binding.
	sudoersUserNameKey = "username"
	sudoersUserIDKey   = "userid"
	// visudoParsedSuffix ends each line visudo -c prints for a file it read
	// (under LC_ALL=C), which is how sudo itself lists every include.
	visudoParsedSuffix = ": parsed OK"
	// noMatchStatus is the exit grep, pgrep, and cmp use for "no match" or
	// "differ", and getentNotFoundStatus is getent's "no such entry". Any
	// other non-zero exit is a failed read, which must stop the removal:
	// read as "no match", it would delete an account with running
	// processes, skip a drop-in that grants it sudo, or archive a shared
	// drop-in as if every rule in it were the account's.
	noMatchStatus        = "1"
	getentNotFoundStatus = "2"
)

// imageUserNamePattern is the portable account-name set useradd accepts. The
// names come from provider adapters, never the host, and this check keeps a
// typo in an adapter from reaching a root shell as anything but a name.
var imageUserNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// ImageUsersRemovalScript removes the login accounts a provider's stock image
// creates next to root. Serverpro manages exactly one admin user, so an image
// account is an unmanaged sudo path with its own keys. Provision runs the
// script after the admin user works through sudo, and doctor --fix runs it
// again for hosts created before it existed.
//
// The order keeps every step reversible until the last: lock and expire the
// account first, so no session can start between the process check and the
// removal, archive its home and crontab, move a sudoers.d file away when
// every rule in it is for that account, or else take only that account's
// rules out of the file, and only then userdel. Each sudoers change keeps the
// original in the archive and puts it back when visudo -c then fails. A rule
// left behind would grant sudo to any later account created with the same
// name, so a shared file that still names the account elsewhere (an alias or
// a rule for several accounts) stops the removal before userdel. An account
// with running processes also stops it, because deleting the account would
// orphan them; it stays locked and the message gives the command that puts
// back its password lock, expiry, and shell as they were. Before
// any of that, a rule for the account in /etc/sudoers or in any other file
// sudo reads that is not directly in /etc/sudoers.d stops the removal with
// nothing changed: serverpro does not edit those files, and the rule would
// outlive userdel. The file list comes from visudo -c, so it follows every
// #include, @include, and includedir exactly as sudo does. A rule that names
// the account by uid (#uid) also stops it before any change, since the edits
// match rules by name only. Last, before userdel, sudo's own parser must
// find no rule or Defaults entry left that names the account or its uid, so
// a form the line edits cannot see stops the removal instead of outliving
// it; output it cannot read in full stops it too. All of that reads only the
// sudoers files, so a sudo.conf or nsswitch.conf setting that makes sudo
// read rules from anywhere else, or load or run a helper other than the
// packaged one as root, stops the removal before any account is touched.
// The script never touches root, the admin user, or a uid 0 account, and it
// is a no-op for an account that is already gone.
func ImageUsersRemovalScript(admin string, users []string) (string, error) {
	for _, user := range users {
		if !imageUserNamePattern.MatchString(user) || user == admin || user == "root" {
			return "", fmt.Errorf("refusing to remove image account %q", user)
		}
	}
	quoted := make([]string, len(users))
	for i, user := range users {
		quoted[i] = shell.Quote(user)
	}
	return `set -eu
export LC_ALL=C
admin=` + shell.Quote(admin) + `
archive_root=` + shell.Quote(ImageUsersArchiveDir) + `
stamp=$(date -u +%Y%m%dT%H%M%SZ)
stop() {
  printf '` + imageUserRemovalFailedPrefix + `%s\n' "$1" >&2
  exit 1
}
` + SudoersSourceGapsFunction() + `# without_user_rules prints sudoers file $2 without the rules for $1 alone.
# It joins continued lines first, as sudo does even when blanks follow the
# backslash, so a wrapped rule goes as a whole and leaves no orphaned
# continuation behind. A rule whose user list goes on after $1 ("$1 , other")
# also grants another account, so it stays, and the check of the edited
# file then stops the removal.
without_user_rules() {
  awk -v user="$1" '
    { block = block $0 "\n"; logical = logical $0 }
    /\\[ \t]*$/ { sub(/\\[ \t]*$/, "", logical); next }
    {
      rest = logical
      sub(/^[ \t]+/, "", rest)
      own = substr(rest, 1, length(user)) == user && substr(rest, length(user) + 1) ~ /^[ \t]+[^, \t]/
      if (!own) printf "%s", block
      block = ""; logical = ""
    }
    END { printf "%s", block }
  ' "$2"
}
# without_comments drops comments and blank lines from stdin. #include and
# #includedir are directives, and #<digits> starts a rule for a uid, so they
# stay.
without_comments() {
  sed -e '/^[[:space:]]*#include/b' -e '/^[[:space:]]*#[0-9]/b' -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d'
}
# is_drop_in succeeds only for a file directly in the drop-in directory, the
# one place serverpro edits. An include can name any other path, a nested or
# dot-segment one included, and those are checked as files serverpro does
# not edit.
is_drop_in() {
  case $1 in
    (` + sudoersDropInDir + `/*/*|` + sudoersDropInDir + `/.|` + sudoersDropInDir + `/..) return 1 ;;
    (` + sudoersDropInGlob + `) return 0 ;;
    (*) return 1 ;;
  esac
}
# read_sudoers_policy reads every rule sudo reads into $policy with sudo's
# own parser, and stops when it cannot, so a parse failure cannot pass for
# "nothing names the account".
read_sudoers_policy() {
  [ -x ` + SudoersParserPath + ` ] || stop "cannot check the sudoers rules: ` + SudoersParserPath + ` (sudo package) is missing"
  policy=$(` + SudoersPolicyCommand + ` 2>/dev/null) || stop "sudo's parser cannot read the sudoers rules; fix them with visudo -c first"
}
# policy_names_account succeeds when $policy names account $1 or uid $2 (an
# empty one is not checked), exits ` + noMatchStatus + ` when it names neither, and exits 2 when
# the JSON is not in the shape it reads. The match is on the parser's JSON,
# not on sudoers text, so a user list, alias, or continued line cannot hide
# the name. It is the last gate before userdel, so it fails closed: every
# account key in the JSON must also be one it read whole, and a change to
# the format (other spacing, a quoted uid, an escaped name) stops the
# removal instead of letting a live rule go unseen.
policy_names_account() {
  printf '%s\n' "$policy" | awk -v user="$1" -v uid="$2" -v shape=` + shell.Quote(sudoersPolicyShape) + ` \
    -v name_key=` + shell.Quote(sudoersUserNameKey) + ` -v id_key=` + shell.Quote(sudoersUserIDKey) + ` '
    # keys counts each "key" followed by a colon outside a string value.
    function keys(text, key,    n, at, rest) {
      n = 0
      while ((at = index(text, key)) > 0) {
        rest = substr(text, at + length(key))
        if ((at == 1 || substr(text, at - 1, 1) != "\\") && rest ~ /^[ \t]*:/) n++
        text = rest
      }
      return n
    }
    # names counts each "key": "value" with a plain value, and notes the
    # account.
    function names(text, prefix,    n, at, end, value) {
      n = 0
      while ((at = index(text, prefix)) > 0) {
        text = substr(text, at + length(prefix))
        end = index(text, "\"")
        if (end == 0) continue
        value = substr(text, 1, end - 1)
        if (value ~ /\\/) continue
        n++
        if (user != "" && value == user) found = 1
      }
      return n
    }
    # ids counts each "key": digits, and notes the uid.
    function ids(text, prefix,    n, at, value) {
      n = 0
      while ((at = index(text, prefix)) > 0) {
        text = substr(text, at + length(prefix))
        if (!match(text, /^[0-9]+/)) continue
        value = substr(text, 1, RLENGTH)
        n++
        if (uid != "" && value == uid) found = 1
      }
      return n
    }
    index($0, shape) { shaped = 1 }
    {
      seen = keys($0, "\"" name_key "\"") + keys($0, "\"" id_key "\"")
      read = names($0, "\"" name_key "\": \"") + ids($0, "\"" id_key "\": ")
      if (seen != read) unread = 1
    }
    END {
      if (!shaped || unread) exit 2
      exit found ? 0 : 1
    }
  '
}
# names_user succeeds when a rule or directive in file $2 names account $1,
# exits ` + noMatchStatus + ` when none does, and anything else when it cannot tell. The file
# is read before the pipe, because a failed read inside it would reach grep
# as an empty file.
names_user() {
  content=$(without_comments <"$2") || return 2
  printf '%s\n' "$content" | grep -Eq "(^|[^a-z0-9_-])$1([^a-z0-9_-]|\$)"
}
# exit_status runs a command and prints only its exit status, so a caller
# can tell a no-match exit from a failed read without set -e ending the
# script.
exit_status() {
  status=0
  "$@" >/dev/null || status=$?
  printf '%s' "$status"
}
# quote prints $1 as one single-quoted shell word, so an undo command built
# from account fields can be pasted as printed, an empty field included.
quote() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}
# account_exists succeeds when account $1 exists and stops when the account
# database cannot be read, so a failed read cannot pass for a removal.
account_exists() {
  case $(exit_status getent passwd "$1") in
    (0) return 0 ;;
    (` + getentNotFoundStatus + `) return 1 ;;
    (*) stop "cannot read the account database" ;;
  esac
}
# A candidate left by an early exit would keep a copy of sudoers rules.
candidate=
trap '[ -z "$candidate" ] || rm -f "$candidate"' EXIT
# outside_sudoers_d stops when a sudoers file outside the drop-ins names $1.
outside_sudoers_d() {
  parsed=$(visudo -c 2>&1) || stop "visudo -c fails before any change; fix the sudoers files first"
  files=$(printf '%s\n' "$parsed" | sed -n ` + shell.Quote("s/"+visudoParsedSuffix+"$//p") + `) || stop "cannot list the sudoers files"
  seen_main=
  set -f
  old_ifs=$IFS
  IFS='
'
  for sudoers_file in $files; do
    [ "$sudoers_file" != ` + shell.Quote(sudoersMainPath) + ` ] || seen_main=1
    ! is_drop_in "$sudoers_file" || continue
    case $(exit_status names_user "$1" "$sudoers_file") in
      (0) stop "$sudoers_file names $1, and serverpro does not edit that file; take $1 out of it with visudo -f $sudoers_file and rerun" ;;
      (` + noMatchStatus + `) ;;
      (*) stop "cannot read $sudoers_file" ;;
    esac
  done
  IFS=$old_ifs
  set +f
  [ -n "$seen_main" ] || stop "visudo -c did not list ` + sudoersMainPath + `"
}
remove_image_user() {
  user=$1
  dest="$archive_root/$user-$stamp"
  outside_sudoers_d "$user"
  uid=
  unlock=
  status=0
  entry=$(getent passwd "$user") || status=$?
  case $status in
    (0|` + getentNotFoundStatus + `) ;;
    (*) stop "cannot read the account database (getent exit $status)" ;;
  esac
  if [ "$status" = 0 ]; then
    uid=$(printf '%s\n' "$entry" | cut -d: -f3)
    [ "$uid" != 0 ] || stop "$user has uid 0"
    read_sudoers_policy
    case $(exit_status policy_names_account '' "$uid") in
      (0) stop "a sudoers rule names $user by uid #$uid, and serverpro removes rules by name only; take it out with visudo and rerun" ;;
      (` + noMatchStatus + `) ;;
      (*) stop "sudo's parse of the sudoers rules is not in the form serverpro reads, so nothing was changed" ;;
    esac
    login_shell=$(printf '%s\n' "$entry" | cut -d: -f7)
    # The undo command puts back what the lock changes: the expiry and shell
    # as they were (empty means none and the default shell), and the
    # password lock only when the password was not locked already. Only the
    # first character of the password field is read, never printed.
    shadow=$(getent shadow "$user") || stop "cannot read the shadow entry of $user"
    expiry=$(printf '%s\n' "$shadow" | cut -d: -f8)
    case $(printf '%s\n' "$shadow" | cut -d: -f2) in
      ('!'*) unlock_flag= ;;
      (*) unlock_flag='-U ' ;;
    esac
    shadow=
    unlock="usermod ${unlock_flag}-e $(quote "$expiry") -s $(quote "$login_shell") $user"
    usermod -L -e 1 -s /usr/sbin/nologin "$user"
    case $(exit_status pgrep -u "$user") in
      (0) stop "$user has running processes and is now locked; end them (loginctl terminate-user $user) and rerun, or undo the lock ($unlock)" ;;
      (` + noMatchStatus + `) ;;
      (*) stop "cannot list the processes of $user, which is now locked; rerun, or undo the lock ($unlock)" ;;
    esac
    install -d -m 0700 "$archive_root" "$dest"
    home=$(printf '%s\n' "$entry" | cut -d: -f6)
    if [ -d "$home" ] && [ "$home" != / ]; then
      tar -C "$home" -czf "$dest/home.tar.gz" .
    fi
    if [ -f "/var/spool/cron/crontabs/$user" ]; then
      mv "/var/spool/cron/crontabs/$user" "$dest/crontab"
    fi
  fi
  for file in ` + sudoersDropInGlob + `; do
    [ -f "$file" ] || continue
    case $(exit_status names_user "$user" "$file") in
      (0) ;;
      (` + noMatchStatus + `) continue ;;
      (*) stop "cannot read $file" ;;
    esac
    name=$(basename "$file")
    others=$(without_user_rules "$user" "$file") || stop "cannot read the other rules in $file"
    others=$(printf '%s' "$others" | without_comments) || stop "cannot read the other rules in $file"
    if [ -z "$others" ]; then
      install -d -m 0700 "$archive_root" "$dest"
      mv "$file" "$dest/"
      if ! visudo -c >/dev/null 2>&1; then
        mv "$dest/$name" "$file" || stop "visudo -c failed without $file, and moving it back from $dest failed"
        stop "visudo -c failed without $file; restored it"
      fi
      printf 'moved %s to %s\n' "$file" "$dest"
      continue
    fi
    # The candidate's name has a dot, so sudo skips it while it is checked.
    candidate=$(mktemp "$file.serverpro.XXXXXX") || stop "cannot create a candidate next to $file"
    without_user_rules "$user" "$file" >"$candidate" || stop "cannot write the candidate for $file"
    case $(exit_status names_user "$user" "$candidate") in
      (0) stop "$file still names $user outside its own rules (an alias or a rule for several accounts); remove $user there with visudo -f $file and rerun" ;;
      (` + noMatchStatus + `) ;;
      (*) stop "cannot read the candidate for $file" ;;
    esac
    # Only a comment named the account, so the file grants it nothing.
    case $(exit_status cmp -s "$candidate" "$file") in
      (0)
        rm -f "$candidate"
        candidate=
        continue
        ;;
      (` + noMatchStatus + `) ;;
      (*) stop "cannot compare the candidate for $file" ;;
    esac
    visudo -cf "$candidate" >/dev/null 2>&1 || stop "$file without the rules for $user fails visudo -c; remove them with visudo -f $file and rerun"
    install -d -m 0700 "$archive_root" "$dest"
    cp -p "$file" "$dest/$name"
    chown root:root "$candidate"
    chmod 0440 "$candidate"
    mv "$candidate" "$file"
    candidate=
    if ! visudo -c >/dev/null 2>&1; then
      cp -p "$dest/$name" "$file" || stop "visudo -c failed after the rules for $user left $file, and restoring it from $dest failed"
      stop "visudo -c failed after the rules for $user left $file; restored it"
    fi
    printf 'removed the rules for %s from %s; the original is in %s\n' "$user" "$file" "$dest"
  done
  read_sudoers_policy
  case $(exit_status policy_names_account "$user" "$uid") in
    (0) stop "sudo still reads a rule for $user after the sudoers edits; remove it with visudo and rerun${unlock:+, or undo the lock ($unlock)}" ;;
    (` + noMatchStatus + `) ;;
    (*) stop "sudo's parse of the sudoers rules is not in the form serverpro reads, so $user stays${unlock:+; undo the lock ($unlock)}" ;;
  esac
  if account_exists "$user"; then
    # userdel exits non-zero for a missing mail spool after it removed the
    # account, so the account's presence decides, not the exit status.
    if ! userdel_out=$(userdel -r "$user" 2>&1); then
      if account_exists "$user"; then
        stop "userdel $user failed: $userdel_out"
      fi
    fi
    printf 'removed image account %s; archive in %s\n' "$user" "$dest"
  else
    printf 'image account %s absent\n' "$user"
  fi
}
[ "$admin" != root ] || stop "admin user is root"
# Every check below reads the sudoers files alone. When sudo can take rules
# from elsewhere or run a swapped helper as root, none of them proves an
# account holds nothing, so nothing changes; the gaps are joined so one stop
# line names them all.
gaps=$(sudoers_source_gaps '') || stop "cannot read ` + sudoConfPath + ` or ` + nsswitchConfPath + `"
[ -z "$gaps" ] || stop "sudo is set up beyond what serverpro checks, so nothing was changed: $(printf '%s\n' "$gaps" | awk 'NR > 1 { printf "; " } { printf "%s", $0 }')"
for user in ` + strings.Join(quoted, " ") + `; do
  remove_image_user "$user"
done`, nil
}

// removeImageUsers runs after the admin password step proved that the admin
// user works through sudo, so removing the image account cannot strand the
// operator. Providers without image accounts skip it.
func removeImageUsers(ctx context.Context, r remote.Runner, creator ComputeCreator, cfg config.Config, st state.State) error {
	declared, ok := creator.(compute.ImageDefaultUsersProvider)
	if !ok || len(declared.ImageDefaultUsers()) == 0 || r == nil || st.Tailscale.Name == "" {
		return nil
	}
	script, err := ImageUsersRemovalScript(cfg.Admin.Username, declared.ImageDefaultUsers())
	if err != nil {
		return err
	}
	if _, err := r.Run(ctx, cfg.Admin.Username, st.Tailscale.Name, script); err != nil {
		return fmt.Errorf("remove provider image accounts: %w", err)
	}
	return nil
}
