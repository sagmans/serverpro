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
// The order keeps every step reversible until the last: lock the account so
// no new session starts, archive its home and crontab, move a sudoers.d file
// away only when every rule in it is for that account (and put it back when
// visudo -c then fails), and only then userdel. An account with running
// processes stops the removal, because deleting it would orphan them. The
// script never touches root, the admin user, or a uid 0 account, and it is a
// no-op for an account that is already gone.
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
# rules_for_others prints the sudoers rules in a file that are not for $1.
# #include and #includedir are directives, not comments, so they count.
rules_for_others() {
  sed -e '/^[[:space:]]*#include/b' -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' "$2" |
    awk -v user="$1" '{ split($0, field, /[[:space:]]+/); token = (field[1] == "" ? field[2] : field[1]); if (token != user) print }'
}
remove_image_user() {
  user=$1
  dest="$archive_root/$user-$stamp"
  if entry=$(getent passwd "$user"); then
    uid=$(printf '%s\n' "$entry" | cut -d: -f3)
    [ "$uid" != 0 ] || stop "$user has uid 0"
    if pgrep -u "$user" >/dev/null 2>&1; then
      stop "$user has running processes; end them (loginctl terminate-user $user) and rerun"
    fi
    usermod -L -e 1 -s /usr/sbin/nologin "$user"
    install -d -m 0700 "$archive_root" "$dest"
    home=$(printf '%s\n' "$entry" | cut -d: -f6)
    if [ -d "$home" ] && [ "$home" != / ]; then
      tar -C "$home" -czf "$dest/home.tar.gz" .
    fi
    if [ -f "/var/spool/cron/crontabs/$user" ]; then
      mv "/var/spool/cron/crontabs/$user" "$dest/crontab"
    fi
  fi
  for file in /etc/sudoers.d/*; do
    [ -f "$file" ] || continue
    grep -Eq "(^|[^a-z0-9_-])$user([^a-z0-9_-]|\$)" "$file" || continue
    if [ -n "$(rules_for_others "$user" "$file")" ]; then
      printf 'kept %s: it has rules for other accounts\n' "$file"
      continue
    fi
    install -d -m 0700 "$archive_root" "$dest"
    mv "$file" "$dest/"
    if ! visudo -c >/dev/null 2>&1; then
      mv "$dest/$(basename "$file")" "$file"
      stop "visudo -c failed without $file; restored it"
    fi
    printf 'moved %s to %s\n' "$file" "$dest"
  done
  if getent passwd "$user" >/dev/null; then
    # userdel exits non-zero for a missing mail spool after it removed the
    # account, so the account's presence decides, not the exit status.
    if ! userdel_out=$(userdel -r "$user" 2>&1); then
      if getent passwd "$user" >/dev/null; then
        stop "userdel $user failed: $userdel_out"
      fi
    fi
    printf 'removed image account %s; archive in %s\n' "$user" "$dest"
  else
    printf 'image account %s absent\n' "$user"
  fi
}
[ "$admin" != root ] || stop "admin user is root"
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
