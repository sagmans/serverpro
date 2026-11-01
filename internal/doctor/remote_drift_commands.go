package doctor

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/cloudinit"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/network"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/shell"
)

// Drift probes print one line per finding with one of these prefixes and
// exit 0 whenever they could read the host. Exit status alone cannot tell
// drift from a probe that never ran (a batch transport error looks the same),
// so the verdict comes from the output and a non-zero exit always means
// "could not evaluate".
const (
	driftFailPrefix = "fail: "
	driftWarnPrefix = "warn: "
)

const (
	egressRestrictedCheckName   = "egress restricted"
	egressRestrictedRemediation = "restricted egress is not in force: confirm no workload needs other outbound ports, then run serverpro server doctor --fix to re-apply the serverpro egress lockdown (USAGE.md, Restricted egress)"

	rebootPendingCheckName = "reboot pending"
	rebootRequiredPath     = rebootgate.RebootRequiredPath
	rebootRequiredPkgsPath = rebootRequiredPath + ".pkgs"
	// kernelImagePrefix is where Ubuntu installs one image per kernel
	// package; the bare /boot/vmlinuz symlinks do not match the prefix.
	kernelImagePrefix = "/boot/vmlinuz-"
	// rebootCommand is the graceful reboot doctor names but never runs: a
	// reboot needs a workload window only the operator knows. It runs on the
	// host, not through serverpro server restart, because some providers'
	// reboot API is a hard reset that can cut an apt or workload write.
	rebootCommand = "sudo systemctl reboot"
	// rebootPendingPrefix states that doctor itself never reboots, with or
	// without the gate.
	rebootPendingPrefix = "updates wait for a reboot, and doctor never reboots: "

	unattendedUpgradesConfigCheckName   = "unattended upgrades config"
	unattendedUpgradesConfigRemediation = "serverpro server doctor --fix restores " + cloudinit.UnattendedUpgradesConfigPath + " (serverpro owns that file; put local options in a later-sorting file); if a value stays wrong, a later apt.conf.d file overrides it: find it with apt-config dump and remove that line"

	privilegedAccountsCheckName   = "privileged accounts"
	privilegedAccountsRemediation = "serverpro server doctor --fix removes only the compute provider's own image accounts and never changes any other account; keep any other listed account (it may be a CI deploy user) and confirm what runs as it: limit a rule that grants sudo without a command limit to the absolute command paths it needs, replace a root-equivalent command (a shell, editor, pager, interpreter, file writer, package, service, or container manager) with a root-owned wrapper script that the account cannot write and that runs one fixed action, take the account out of a privileged group it does not need (gpasswd -d USER GROUP), and run visudo -c after each sudoers change"
	// sudoersListedSeparator introduces the paths in a listed-commands
	// finding.
	sudoersListedSeparator = " for listed commands: "
	// sudoCommandMaxHops is the symlink limit the Linux kernel applies
	// (MAXSYMLINKS), so the path walk gives up where sudo's exec would.
	sudoCommandMaxHops = 40

	metadataUserDataCheckName = "metadata user-data"
	// metadataServiceURL is the link-local metadata service every supported
	// compute provider serves; the provider adapter supplies the path.
	metadataServiceURL          = "http://169.254.169.254"
	metadataConnectTimeoutSecs  = 3
	metadataMaxTimeSecs         = 10
	metadataHTTPOK              = "200"
	metadataHTTPNotFound        = "404"
	metadataUserDataRemediation = "any local process can read metadata user-data: if the admin password is still the one set at provisioning, change it to a new value (re-hashing the same password does not help); then remove the hash from the server's user-data through the provider (update it and reboot where the provider allows, otherwise rebuild or replace the server)"
	// metadataUserDataUnchecked is the skip evidence when the compute provider
	// declares no metadata path, so the gap stays visible in the report.
	metadataUserDataUnchecked = "compute provider declares no metadata user-data path"
	// cryptHashPattern matches crypt(3) strings of every scheme Ubuntu or an
	// older serverpro release could have written (MD5, bcrypt, SHA, scrypt,
	// yescrypt), without depending on the YAML key that carried it.
	cryptHashPattern = `\$(1|2[aby]|5|6|7|y|gy)\$[./0-9A-Za-z$]+`
	// passwdKeyPattern catches cloud-config password keys whose value is not
	// a recognizable crypt string, such as a plain-text passwd.
	passwdKeyPattern = `^[[:space:]-]*(hashed_)?passwd:`

	tailscaleOperatorCheckName   = "tailscale operator"
	tailscaleOperatorRemediation = "clear it with: sudo tailscale set --operator= (an operator reconfigures tailscaled, including Serve and Funnel, without the sudo password)"

	tailscaleServeCheckName = "tailscale serve"
	// tailscaleServeSectionMarker and listenersSectionMarker split the serve
	// probe output, which carries two documents, into its parts.
	tailscaleServeSectionMarker = "--- serve"
	listenersSectionMarker      = "--- listeners"
	// serveConfigWithheld replaces the serve config wherever a failed or
	// malformed read would otherwise put it in evidence, which is saved to
	// disk: the config names tailnet hosts, paths, and backends.
	serveConfigWithheld = "(serve config not shown)"

	sysctlRuntimeCheckName   = "sysctl runtime"
	sysctlRuntimeRemediation = "re-apply the file (sudo sysctl -p " + cloudinit.SysctlConfigPath + "); if the value drifts again, find the later writer (systemd-sysctl order, networkd, provider agents) before the next reboot"

	// privilegedGroupsCheckedLead starts the privileged accounts probe's
	// last line, which names the scanned groups so a clean result still
	// shows what was looked at.
	privilegedGroupsCheckedLead = "checked groups: "
)

// Stock-host answer of the privileged accounts probe, for remote fakes. The
// policy is what cvtsudoers prints for Ubuntu's packaged /etc/sudoers, which
// every provider image starts from, and the admin uid is the first one
// useradd hands out there.
const (
	privilegedAccountsIdleSecurePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin"
	privilegedAccountsIdleAdminUID   = "1000"
	privilegedAccountsIdlePolicy     = `{"Defaults": [{"Options": [{"env_reset": true}]}, {"Options": [{"mail_badpass": true}]}, {"Options": [{"secure_path": "` + privilegedAccountsIdleSecurePath + `"}]}, {"Options": [{"use_pty": true}]}], ` +
		`"User_Specs": [` +
		`{"User_List": [{"username": "root"}], "Host_List": [{"hostname": "ALL"}], "Cmnd_Specs": [{"runasusers": [{"username": "ALL"}], "runasgroups": [{"usergroup": "ALL"}], "Options": [{"setenv": true}], "Commands": [{"command": "ALL"}]}]}, ` +
		`{"User_List": [{"usergroup": "admin"}], "Host_List": [{"hostname": "ALL"}], "Cmnd_Specs": [{"runasusers": [{"username": "ALL"}], "Options": [{"setenv": true}], "Commands": [{"command": "ALL"}]}]}, ` +
		`{"User_List": [{"usergroup": "sudo"}], "Host_List": [{"hostname": "ALL"}], "Cmnd_Specs": [{"runasusers": [{"username": "ALL"}], "runasgroups": [{"usergroup": "ALL"}], "Options": [{"setenv": true}], "Commands": [{"command": "ALL"}]}]}` +
		`]}`
)

// privilegedGroups grant root or a root-equivalent: sudo and admin through
// Ubuntu's sudoers, lxd and docker through privileged containers.
var privilegedGroups = []string{"sudo", "admin", "lxd", "docker"}

// rootEquivalentCommands are command names that hand out a root shell or
// root file writes even when a sudoers rule lists them by absolute path: a
// shell or interpreter runs anything, an editor or pager escapes to a shell,
// a file writer can rewrite sudoers, and a package, service, or container
// manager runs code as root. Fixed arguments do not make them safe, because
// a pager or editor still opens and sudo's argument matching is easy to get
// wrong, so a rule that names one fails whatever its arguments. The probe
// matches on the base name normalized by commandNamePatterns, so
// /usr/bin/python3.12, /usr/bin/vim.basic, and /usr/bin/sqlite3 match too;
// each entry is therefore the normalized name (sqlite, not sqlite3). The
// list follows the sudo entries in GTFOBins (see WEB_SOURCES.md) for tools
// commonly found on Ubuntu servers, plus the Debian alternatives (editor,
// pager) that point at one of them.
var rootEquivalentCommands = []string{
	// Shells and command runners.
	"sh", "bash", "dash", "ash", "zsh", "ksh", "csh", "tcsh", "fish", "busybox",
	"rbash", "su", "sudo", "pkexec", "env", "nice", "nohup", "timeout",
	"stdbuf", "xargs", "find", "script", "expect", "tmux", "screen", "watch",
	"flock", "setsid", "chroot", "nsenter", "unshare", "setpriv", "runuser",
	"capsh", "strace", "gdb", "ssh", "socat", "start-stop-daemon", "run-parts",
	"ip", "tcpdump",
	// Editors and pagers.
	"vi", "vim", "view", "rvim", "nvim", "nano", "ed", "ex", "emacs",
	"editor", "sensible-editor", "vipw", "vigr",
	"less", "more", "most", "man", "pager", "journalctl",
	// Interpreters, compilers, and database clients.
	"awk", "gawk", "mawk", "nawk", "sed", "perl", "python", "ruby", "lua",
	"node", "nodejs", "php", "tclsh", "wish", "gcc", "cc", "sqlite", "psql",
	"mysql",
	// File writers and permission changers.
	"tee", "dd", "cp", "mv", "install", "ln", "rsync", "tar", "zip", "unzip",
	"cpio", "chmod", "chown", "chgrp", "setfacl", "curl", "wget", "scp",
	"sftp", "mount", "openssl", "debugfs",
	// Account, package, service, and container managers.
	"passwd", "chpasswd", "gpasswd", "usermod", "useradd", "adduser",
	"visudo", "crontab", "apt", "apt-get", "dpkg", "update-alternatives",
	"snap", "pip", "npm", "gem", "make", "git", "systemctl", "systemd-run",
	"systemd-nspawn", "busctl", "loginctl", "service", "docker", "podman",
	"ctr", "nerdctl", "kubectl", "lxc",
	// Commands that read or write any file, load kernel code or file
	// capabilities, open a network shell, trace or load a program, run a
	// job later, or take a config path; ld is the dynamic loader (ld.so)
	// and the linker, both of which run or write chosen code.
	"sort", "uniq", "shuf", "gpg", "setcap", "insmod", "modprobe", "kmod",
	"ssh-keygen", "nc", "ncat", "ltrace", "valgrind", "at",
	"systemd-tmpfiles", "logrotate", "ld", "vimdiff", "rview", "mksh",
}

// rootEquivalentPrefixes match normalized names whose suffix names a CPU
// architecture: the dynamic loader is ld-linux-x86-64.so.2 or
// ld-linux-aarch64.so.1, which normalize to different names.
var rootEquivalentPrefixes = []string{"ld-linux"}

// commandNamePatterns turn a command's base name into the name
// rootEquivalentCommands holds, in order: a GNU target prefix
// (x86_64-linux-gnu-gcc-13 is what /usr/bin/gcc resolves to), a dotted
// suffix (vim.basic, python3.12), trailing version digits (sqlite3), and a
// dash those digits leave (gcc-13). Go applies them to a listed path and
// the host's sed to the path it resolves to, so each pattern only uses
// syntax that RE2 and POSIX basic regular expressions read the same way,
// and none holds a "/".
var commandNamePatterns = []string{`^[a-z0-9_]*-linux-gnu[a-z]*-`, `[.].*$`, `[0-9]*$`, `-$`}

// commandNameSed is the sed expression arguments for the normalization Go
// applies with commandNameExpressions.
func commandNameSed() string {
	var expressions strings.Builder
	for _, pattern := range commandNamePatterns {
		expressions.WriteString(" -e " + shell.Quote("s/"+pattern+"//"))
	}
	return expressions.String()
}

// sudoersPathChecks is the shell that prints the host's verdict on every
// path the Defaults path options name, read out of cvtsudoers' JSON with
// sed. A value with an escaped character does not match, gets no verdict,
// and Go fails it. secure_path is split into its directories the way sudo
// splits it, and each line is checked as the kind its option names.
func sudoersPathChecks() string {
	var checks strings.Builder
	for _, name := range sudoersPathOptions {
		kind, split := sudoersPathDirectory, ""
		switch sudoersSafeDefaults[name] {
		case defaultsFile:
			kind = sudoersPathFile
		case defaultsDirectoryList:
			split = " | tr " + shell.Quote(sudoersSecurePathSeparator) + " '\\n'"
		}
		checks.WriteString("printf '%s\\n' \"$sudoers\" | sed -n " + shell.Quote(`s/^.*"`+name+`": "\([^"\\]*\)".*$/\1/p`) + split +
			" | while IFS= read -r value; do check_path " + kind + " \"$value\" || exit 1; done || exit 1\n")
	}
	return checks.String()
}

// getentNotFoundStatus and grepNoMatchStatus are the exits that mean "no
// such entry" rather than a failed read: the privileged accounts scan skips
// only those, so a broken account database cannot read as an empty one.
const (
	getentNotFoundStatus = "2"
	grepNoMatchStatus    = "1"
)

// egressRestrictedCommand checks the policy and the effect separately: UFW
// can report "deny (outgoing)" while an ALLOW OUT Anywhere rule, a second
// firewall, or a missed reload still lets traffic out, so a real connect to a
// port the lockdown never allows is the proof. It also lists every lockdown
// rule that is missing, because a deny default without them breaks apt,
// tunnels, and Tailscale on the next reconnect.
func egressRestrictedCommand() string {
	// The probe target is the lockdown's own proof, so doctor and
	// provisioning cannot disagree on whether restricted egress is in force.
	target := network.EgressBlockedProbeHost + ":" + strconv.Itoa(network.EgressBlockedProbePort)
	return "export LC_ALL=C\n" +
		"status=$(ufw status verbose) || exit 1\n" +
		"outgoing=$(printf '%s\\n' \"$status\" | sed -n 's/^Default:.*[[:space:]]\\([a-z]*\\) (outgoing).*/\\1/p')\n" +
		"case \"$outgoing\" in\n" +
		"  deny|reject) printf 'ufw default outgoing: %s\\n' \"$outgoing\" ;;\n" +
		"  *) printf '" + driftFailPrefix + "ufw default outgoing is %s, want deny\\n' \"${outgoing:-unset (ufw inactive?)}\" ;;\n" +
		"esac\n" +
		egressAllowRulesCommand() +
		"if timeout " + strconv.Itoa(network.EgressBlockedProbeTimeoutSeconds) + " bash -c " + shell.Quote("exec 3<>/dev/tcp/"+network.EgressBlockedProbeHost+"/"+strconv.Itoa(network.EgressBlockedProbePort)) + " 2>/dev/null; then\n" +
		"  printf '%s\\n' " + shell.Quote(driftFailPrefix+"tcp "+target+" connected; non-allowlisted egress is open") + "\n" +
		"else\n" +
		"  printf '%s\\n' " + shell.Quote("tcp "+target+" blocked") + "\n" +
		"fi"
}

// egressAllowRulesCommand prints one finding per lockdown rule missing from
// the ufw status already read into $status. The rule leads the line in ufw's
// "To" column, so an anchored match cannot hit a port inside another rule.
func egressAllowRulesCommand() string {
	var command strings.Builder
	for _, rule := range network.RestrictedAllowOut() {
		command.WriteString("printf '%s\\n' \"$status\" | grep -Eq " + shell.Quote("^(\\[ *[0-9]+\\] )?"+regexp.QuoteMeta(rule)+"( \\(v6\\))?[[:space:]]+ALLOW OUT") + " || printf '%s\\n' " + shell.Quote(driftFailPrefix+"ufw allow out "+rule+" missing") + "\n")
	}
	return command.String()
}

// rebootPendingRemediation names the exact commands for this server, so the
// operator does not have to work out how to reach it.
func rebootPendingRemediation(namespace, server string) string {
	return rebootPendingPrefix + "when the workload allows, " + manualRebootSteps(namespace, server)
}

// rebootPendingRemediationFor names the gated reboot first when config turns
// it on, because the pending reboot then happens in the window without the
// operator once reboot survivability shows no gap.
func rebootPendingRemediationFor(cfg config.Config) string {
	if !cfg.Reboot.Enabled {
		return rebootPendingRemediation(cfg.Namespace, cfg.Server)
	}
	return rebootPendingPrefix + "the gated reboot runs at " + cfg.Reboot.Window + " UTC once " + rebootSurvivabilityCheckName + " passes; to reboot sooner, " + manualRebootSteps(cfg.Namespace, cfg.Server)
}

func manualRebootSteps(namespace, server string) string {
	return "run serverpro server ssh " + server + " -n " + namespace + ", then " + rebootCommand + "; rerun doctor after it is back"
}

// unattendedUpgradesConfigCommand reads the effective value of every option
// the serverpro drop-in sets, not the file: a later apt.conf.d file or an
// edited conffile wins over the drop-in, and only apt-config shows which
// value unattended-upgrades will use. An unset value means the drop-in is
// gone and the package's in-code default applies, which serverpro does not
// pin, so it is a warning.
func unattendedUpgradesConfigCommand() string {
	var command strings.Builder
	command.WriteString("export LC_ALL=C\n")
	for _, setting := range cloudinit.UnattendedUpgradesSettings() {
		want := cloudinit.AptBool(setting.Value)
		command.WriteString("value=$(apt-config shell value " + shell.Quote(setting.Key+"/b") + ") || exit 1\n" +
			"case \"$value\" in\n" +
			"  " + shell.Quote("value='"+want+"'") + ") printf '%s\\n' " + shell.Quote(setting.Key+" is "+want) + " ;;\n" +
			"  '') printf '%s\\n' " + shell.Quote(driftWarnPrefix+setting.Key+" is not set; the package default applies") + " ;;\n" +
			"  *) printf '%s\\n' " + shell.Quote(driftFailPrefix+setting.Key+" is not "+want) + " ;;\n" +
			"esac\n")
	}
	return command.String()
}

// unattendedUpgradesConfigFixCommand rewrites the serverpro drop-in with the
// bytes cloud-init writes. The temporary file sits in the parent of
// apt.conf.d: the same filesystem keeps the rename atomic, and APT never
// parses a half-written file there.
func unattendedUpgradesConfigFixCommand() string {
	scratchDir := path.Dir(path.Dir(cloudinit.UnattendedUpgradesConfigPath))
	return "set -eu\n" +
		"umask 022\n" +
		"tmp=$(mktemp " + shell.Quote(scratchDir+"/.serverpro-unattended-upgrades.") + "XXXXXX)\n" +
		"trap 'rm -f \"$tmp\"' EXIT\n" +
		"printf '%s' " + shell.Quote(cloudinit.UnattendedUpgradesConfig()) + " > \"$tmp\"\n" +
		"chmod 0644 \"$tmp\"\n" +
		"mv -f \"$tmp\" " + shell.Quote(cloudinit.UnattendedUpgradesConfigPath)
}

// rebootPendingCommand reports both Ubuntu's own flag and a newer installed
// kernel, because a kernel installed outside unattended-upgrades (or a flag
// file removed by hand) leaves only one of the two signals.
func rebootPendingCommand() string {
	return "export LC_ALL=C\n" +
		"running=$(uname -r)\n" +
		"newest=$(for image in " + kernelImagePrefix + "*; do [ -e \"$image\" ] && printf '%s\\n' \"${image#" + kernelImagePrefix + "}\"; done | sort -V | tail -n 1)\n" +
		"if [ -e " + rebootRequiredPath + " ]; then\n" +
		"  packages=\n" +
		"  if [ -r " + rebootRequiredPkgsPath + " ]; then packages=$(sort -u " + rebootRequiredPkgsPath + " | tr '\\n' ' '); fi\n" +
		"  printf '" + driftWarnPrefix + "%s present%s\\n' " + rebootRequiredPath + " \"${packages:+ for ${packages% }}\"\n" +
		"fi\n" +
		"if [ -n \"$newest\" ] && [ \"$newest\" != \"$running\" ] && [ \"$(printf '%s\\n%s\\n' \"$running\" \"$newest\" | sort -V | tail -n 1)\" = \"$newest\" ]; then\n" +
		"  printf '" + driftWarnPrefix + "running kernel %s is older than installed kernel %s\\n' \"$running\" \"$newest\"\n" +
		"fi\n" +
		"printf 'running kernel %s\\n' \"$running\""
}

// privilegedAccountsCommand lists every account other than the admin user
// that holds root through a group, a sudoers rule, or uid 0. It counts
// primary-group members too, because getent group lists only supplementary
// ones, and provider images add their default user (for example linuxuser)
// this way or the other. It also lists each provider image account that still
// exists, privileged or not, because it is a second login path with its own
// keys that serverpro does not manage.
//
// sudo's own parser (cvtsudoers) reads the rules and gradePrivilegedAccounts
// grades its JSON in Go. The host only answers what Go cannot: for each
// absolute path the JSON lists, whether only root can change what it runs,
// and for each absolute path a Defaults path option names (secure_path,
// logfile, iolog_dir), whether only root can change it.
// The walk starts at / and checks every component and every symlink hop,
// and each link's directory, as the kernel resolves them: a symlink such as
// /usr/local/bin/deploy pointing at bash, a link in a directory the account
// can write, or a script the account can rewrite runs anything as root
// while its listed path looks narrow. A file named like the path plus a
// space would be what sudo runs if the path held an escaped space, which
// the JSON cannot show, so it fails too. The paths come out of the JSON
// with sed, which takes only the text before the first space: arguments
// often hold JSON-escaped quotes or backslashes (a digest regex, an empty
// "" argument), and requiring the whole command to be escape-free left safe
// wrappers unchecked. A path with an escaped character still does not
// match, gets no verdict, and Go fails it.
//
// cvtsudoers reads only /etc/sudoers and its includes, so the probe also
// reports each sudo.conf or nsswitch.conf setting that lets sudo take rules
// from elsewhere, or load or run a helper other than the packaged one as
// root: grades of the parse alone would miss what those settings grant.
//
// Each scan is captured and its status checked on its own, and the account
// database is read once: in a pipeline without pipefail a scan that failed
// part way would still exit 0 through sort, and a partial list would read as
// a host with nothing to report. The case patterns use the (pattern) form,
// which a case inside $(...) needs so a bare ")" cannot end the substitution.
func privilegedAccountsCommand(admin string, imageUsers []string) string {
	imageAccounts := "images=\n"
	if len(imageUsers) > 0 {
		quoted := make([]string, len(imageUsers))
		for i, user := range imageUsers {
			quoted[i] = shell.Quote(user)
		}
		imageAccounts = "images=$(\n" +
			"for user in " + strings.Join(quoted, " ") + "; do\n" +
			"  status=0\n" +
			"  printf '%s\\n' \"$accounts\" | cut -d: -f1 | grep -Fqx -- \"$user\" || status=$?\n" +
			"  case $status in\n" +
			"    (0) printf '" + driftFailPrefix + "provider image account %s exists\\n' \"$user\" ;;\n" +
			"    (" + grepNoMatchStatus + ") ;;\n" +
			"    (*) exit 1 ;;\n" +
			"  esac\n" +
			"done\n" +
			") || unreadable 'the provider image accounts'\n"
	}
	return "export LC_ALL=C\n" +
		"admin=" + shell.Quote(admin) + "\n" +
		"unreadable() {\n" +
		"  printf 'could not evaluate privileged accounts: cannot read %s\\n' \"$1\"\n" +
		"  exit 1\n" +
		"}\n" +
		lifecycle.SudoersSourceGapsFunction() +
		"root_equivalent_names=" + shell.Quote(strings.Join(rootEquivalentCommands, " ")) + "\n" +
		"root_equivalent_prefixes=" + shell.Quote(strings.Join(rootEquivalentPrefixes, " ")) + "\n" +
		// entry_gap prints why ls -ldn entry $2 of path $1 lets someone other
		// than root change it. A symlink's own mode bits mean nothing, so
		// only its owner counts.
		"entry_gap() {\n" +
		"  set -f; set -- \"$1\" $2; set +f\n" +
		"  case $2 in\n" +
		"    (l*) ;;\n" +
		"    (*+) printf '%s has an ACL' \"$1\"; return 0 ;;\n" +
		"    (?????w*|????????w*) printf '%s is writable by group or others' \"$1\"; return 0 ;;\n" +
		"  esac\n" +
		"  [ \"$4\" = 0 ] || printf '%s is not owned by root' \"$1\"\n" +
		"}\n" +
		// walk_path resolves path $1 from / the way the kernel does and
		// sets real to the result, or to nothing when a component does not
		// exist. It returns 2 after it prints why someone other than root
		// can change a component or a link on the way, and 1 when it cannot
		// read one. dir holds the resolved directory so far, with \"\" for /.
		"walk_path() {\n" +
		"  real=\n" +
		"  entry=$(ls -ldn -- /) || return 1\n" +
		"  gap=$(entry_gap / \"$entry\")\n" +
		"  [ -z \"$gap\" ] || { printf 'whose path %s' \"$gap\"; return 2; }\n" +
		"  rest=$1 dir= hops=0\n" +
		"  while :; do\n" +
		"    while :; do case $rest in (/*) rest=${rest#/} ;; (*) break ;; esac; done\n" +
		"    [ -n \"$rest\" ] || break\n" +
		"    name=${rest%%/*}\n" +
		"    rest=${rest#\"$name\"}\n" +
		"    case $name in\n" +
		"      (.) continue ;;\n" +
		"      (..) dir=${dir%/*}; continue ;;\n" +
		"    esac\n" +
		"    next=$dir/$name\n" +
		"    [ -e \"$next\" ] || [ -L \"$next\" ] || return 0\n" +
		"    entry=$(ls -ldn -- \"$next\") || return 1\n" +
		"    gap=$(entry_gap \"$next\" \"$entry\")\n" +
		"    [ -z \"$gap\" ] || { printf 'whose path %s' \"$gap\"; return 2; }\n" +
		"    case $entry in\n" +
		"      (l*)\n" +
		"        hops=$((hops + 1))\n" +
		"        [ \"$hops\" -le " + strconv.Itoa(sudoCommandMaxHops) + " ] || { printf 'whose symlinks do not end'; return 2; }\n" +
		"        target=$(readlink -- \"$next\") || return 1\n" +
		"        case $target in (/*) dir= ;; esac\n" +
		"        rest=$target$rest\n" +
		"        ;;\n" +
		"      (*) dir=$next ;;\n" +
		"    esac\n" +
		"  done\n" +
		"  real=${dir:-/}\n" +
		"}\n" +
		// path_gap prints why Defaults path $2 of kind $1 (directory or
		// file) is not safe for sudo to search, read, or write as root, or
		// nothing. A path that does not exist yet passes, because the walk
		// proved that only root can create it.
		"path_gap() {\n" +
		"  status=0; walk_path \"$2\" || status=$?\n" +
		"  case $status in (0) ;; (2) return 0 ;; (*) return 1 ;; esac\n" +
		"  [ -n \"$real\" ] || return 0\n" +
		"  entry=$(ls -ldn -- \"$real\") || return 1\n" +
		"  case $1:$entry in\n" +
		"    (" + sudoersPathDirectory + ":d*|" + sudoersPathFile + ":-*) ;;\n" +
		"    (*) printf 'which does not resolve to a %s' \"$1\" ;;\n" +
		"  esac\n" +
		"}\n" +
		// check_path prints one kind<TAB>path<TAB>verdict line. Only plain
		// absolute paths are checked; Go fails every other entry without
		// asking the host.
		"check_path() {\n" +
		"  case $2 in (/*) ;; (*) return 0 ;; esac\n" +
		"  case $2 in (*" + sudoersPathEscape + "*) return 0 ;; esac\n" +
		"  gap=$(path_gap \"$1\" \"$2\") || return 1\n" +
		"  printf '%s\\t%s\\t%s\\n' \"$1\" \"$2\" \"${gap:-" + sudoCommandSafe + "}\"\n" +
		"}\n" +
		// command_gap prints why listed sudo command path $1 is not safe to
		// grant, or nothing.
		"command_gap() {\n" +
		"  for twin in \"$1 \"*; do\n" +
		"    if [ -e \"$twin\" ] || [ -L \"$twin\" ]; then printf 'whose path is ambiguous: %s also exists' \"$twin\"; return 0; fi\n" +
		"  done\n" +
		"  status=0; walk_path \"$1\" || status=$?\n" +
		"  case $status in (0) ;; (2) return 0 ;; (*) return 1 ;; esac\n" +
		"  [ -n \"$real\" ] || { printf 'which does not resolve to a file'; return 0; }\n" +
		"  entry=$(ls -ldn -- \"$real\") || return 1\n" +
		"  case $entry in (-*) ;; (*) printf 'which does not resolve to a file'; return 0 ;; esac\n" +
		"  base=$(printf '%s\\n' \"${real##*/}\" | sed" + commandNameSed() + ") || return 1\n" +
		"  case \" $root_equivalent_names \" in (*\" $base \"*) printf 'which resolves to root-equivalent %s' \"$real\"; return 0 ;; esac\n" +
		"  for prefix in $root_equivalent_prefixes; do\n" +
		"    case $base in (\"$prefix\"*) printf 'which resolves to root-equivalent %s' \"$real\"; return 0 ;; esac\n" +
		"  done\n" +
		"}\n" +
		"accounts=$(getent passwd) || unreadable 'the account database'\n" +
		"[ -x " + lifecycle.SudoersParserPath + " ] || unreadable 'the sudoers rules: " + lifecycle.SudoersParserPath + " (sudo package) is missing'\n" +
		"sudoers=$(" + lifecycle.SudoersPolicyCommand + ") || unreadable 'the sudoers rules: cvtsudoers failed'\n" +
		// Only plain absolute paths are checked; Go fails every other
		// command without asking the host.
		"commands=$(\n" +
		"printf '%s\\n' \"$sudoers\" | sed -n " + shell.Quote(`s/^.*"command": "\([^" \\]*\)[ "].*$/\1/p`) + " | while IFS= read -r cmd; do\n" +
		"  path=${cmd%% *}\n" +
		"  case $path in (/*) ;; (*) continue ;; esac\n" +
		"  case $path in (*[*?[]*) continue ;; esac\n" +
		"  gap=$(command_gap \"$path\") || exit 1\n" +
		"  printf '%s\\t%s\\n' \"$path\" \"${gap:-" + sudoCommandSafe + "}\"\n" +
		"done\n" +
		") || unreadable 'the listed sudo commands'\n" +
		"paths=$(\n" + sudoersPathChecks() + ") || unreadable 'the sudoers paths'\n" +
		"admin_uid=$(printf '%s\\n' \"$accounts\" | awk -F: -v user=\"$admin\" '$1 == user { print $3; exit }') || unreadable 'the admin uid'\n" +
		imageAccounts +
		"groups=$(\n" +
		"for group in " + strings.Join(privilegedGroups, " ") + "; do\n" +
		"  status=0\n" +
		"  entry=$(getent group \"$group\") || status=$?\n" +
		"  [ \"$status\" -eq " + getentNotFoundStatus + " ] && continue\n" +
		"  [ \"$status\" -eq 0 ] || exit 1\n" +
		"  gid=$(printf '%s\\n' \"$entry\" | cut -d: -f3)\n" +
		"  primary=$(printf '%s\\n' \"$accounts\" | awk -F: -v gid=\"$gid\" '$4 == gid { print $1 }') || exit 1\n" +
		"  for member in $(printf '%s\\n' \"$entry\" | cut -d: -f4 | tr ',' ' ') $primary; do\n" +
		"    [ \"$member\" = \"$admin\" ] && continue\n" +
		"    printf '" + driftFailPrefix + "%s is in privileged group %s\\n' \"$member\" \"$group\"\n" +
		"  done\n" +
		"done\n" +
		") || unreadable 'the privileged groups'\n" +
		"uids=$(printf '%s\\n' \"$accounts\" | awk -F: '$3 == 0 && $1 != \"root\" { printf \"" + driftFailPrefix + "%s has uid 0\\n\", $1 }') || unreadable 'the uid 0 accounts'\n" +
		"sources=$(sudoers_source_gaps " + shell.Quote(driftFailPrefix+sudoersSourceFindingLead) + ") || unreadable 'the sudo.conf or nsswitch.conf settings'\n" +
		"printf '%s\\n' " + shell.Quote(sudoersSectionMarker) + " \"$sudoers\" " + shell.Quote(sudoCommandsSectionMarker) + " \"$commands\" " + shell.Quote(sudoersPathsSectionMarker) + " \"$paths\" " + shell.Quote(adminUIDSectionMarker) + " \"$admin_uid\" " + shell.Quote(accountsSectionMarker) + " \"$images\" \"$groups\" \"$uids\" \"$sources\"\n" +
		"printf '" + privilegedGroupsCheckedLead + "%s\\n' " + shell.Quote(strings.Join(privilegedGroups, " "))
}

// PrivilegedAccountsCommand is the privileged accounts probe. It is exported,
// with PrivilegedAccountsIdleOutput, for remote fakes that answer by script:
// the grader fails closed on output without its sections, so a generic
// success string fails the check.
func PrivilegedAccountsCommand(admin string, imageUsers []string) string {
	return privilegedAccountsCommand(admin, imageUsers)
}

// PrivilegedAccountsIdleOutput is PrivilegedAccountsCommand's answer on a
// host with only the packaged sudoers policy, root-owned secure_path
// directories, and no account beyond the admin in a privileged group. Its
// layout follows the probe's final printf lines, one line per value.
func PrivilegedAccountsIdleOutput() string {
	var paths []string
	for _, dir := range strings.Split(privilegedAccountsIdleSecurePath, sudoersSecurePathSeparator) {
		paths = append(paths, sudoersPathDirectory+"\t"+dir+"\t"+sudoCommandSafe)
	}
	return strings.Join([]string{
		sudoersSectionMarker, privilegedAccountsIdlePolicy,
		sudoCommandsSectionMarker, "",
		sudoersPathsSectionMarker, strings.Join(paths, "\n"),
		adminUIDSectionMarker, privilegedAccountsIdleAdminUID,
		// Empty image, group, uid 0, and sudo source findings.
		accountsSectionMarker, "", "", "", "",
		privilegedGroupsCheckedLead + strings.Join(privilegedGroups, " "),
	}, "\n") + "\n"
}

// metadataUserDataCommand reports only whether provider user-data still holds
// a password hash, and whether it is the admin's current one. User-data stays
// readable by every local process for the server's life, and older serverpro
// releases put the admin hash there. The body, hash, and shadow entry stay in
// shell variables and are never printed; the response is held in memory, not
// written to disk, because it also carries the one-time Tailscale auth key.
// path comes from the compute provider adapter, never from the host.
func metadataUserDataCommand(admin, path string) string {
	url := metadataServiceURL + path
	return "export LC_ALL=C\n" +
		"url=" + shell.Quote(url) + "\n" +
		"response=$(curl -sS --connect-timeout " + strconv.Itoa(metadataConnectTimeoutSecs) + " -m " + strconv.Itoa(metadataMaxTimeSecs) + " -w '\\n%{http_code}' \"$url\" 2>/dev/null) || response=\n" +
		"code=$(printf '%s\\n' \"$response\" | tail -n 1)\n" +
		"case \"$code\" in\n" +
		"  " + metadataHTTPOK + ") ;;\n" +
		"  " + metadataHTTPNotFound + ") printf '%s has no user-data\\n' \"$url\"; exit 0 ;;\n" +
		"  *) printf '" + driftWarnPrefix + "%s answered HTTP %s; cannot check user-data for a password hash\\n' \"$url\" \"${code:-none}\"; exit 0 ;;\n" +
		"esac\n" +
		"user_data=$(printf '%s\\n' \"$response\" | sed '$d')\n" +
		"unset response\n" +
		"hash=$(printf '%s\\n' \"$user_data\" | grep -oE " + shell.Quote(cryptHashPattern) + " | head -n 1)\n" +
		"keys=$(printf '%s\\n' \"$user_data\" | grep -cE " + shell.Quote(passwdKeyPattern) + ") || keys=0\n" +
		"unset user_data\n" +
		"if [ -z \"$hash\" ] && [ \"$keys\" = 0 ]; then printf '%s holds no password hash\\n' \"$url\"; exit 0; fi\n" +
		"current=$(getent shadow " + shell.Quote(admin) + " | cut -d: -f2) || current=\n" +
		"if [ -n \"$hash\" ] && [ \"$hash\" = \"$current\" ]; then\n" +
		"  printf '" + driftFailPrefix + "%s holds the current admin password hash (value not shown)\\n' \"$url\"\n" +
		"else\n" +
		"  printf '" + driftWarnPrefix + "%s holds a password hash or passwd key that is not the current admin hash (value not shown)\\n' \"$url\"\n" +
		"fi\n" +
		"unset hash current"
}

// tailscaleOperatorCommand prints only the OperatorUser value. The full prefs
// document is never echoed, not even on error, because doctor evidence is
// saved to disk and the document carries node state.
func tailscaleOperatorCommand() string {
	return "export LC_ALL=C\n" +
		"prefs=$(tailscale debug prefs 2>/dev/null) || { printf '%s\\n' 'tailscale debug prefs failed'; exit 1; }\n" +
		"operator=$(printf '%s\\n' \"$prefs\" | sed -n 's/^[[:space:]]*\"OperatorUser\":[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p' | head -n 1)\n" +
		"unset prefs\n" +
		"if [ -n \"$operator\" ]; then printf '" + driftFailPrefix + "tailscale operator is %s\\n' \"$operator\"; else printf '%s\\n' 'tailscale operator unset'; fi"
}

// TailscaleServeCommand returns the serve config and the listeners in one
// read, so doctor compares both from the same moment; Go parses the JSON
// because the host has no JSON tool doctor can rely on. ss lists TCP and UDP
// so its lines keep the Netid column the listening-ports parser expects.
// Both reads finish before anything prints, so a failed ss never leaves the
// serve config in the error output; tailscale's stderr is dropped for the
// same reason, and the exit status stands in for it.
// It is exported, with TailscaleServeIdleOutput, for remote fakes that answer
// by script: the probe parses its output, so a generic success string fails.
func TailscaleServeCommand() string {
	return "export LC_ALL=C\n" +
		"serve=$(tailscale serve status --json 2>/dev/null) || { status=$?; printf 'tailscale serve status --json failed (exit %s)\\n' \"$status\"; exit 1; }\n" +
		"listeners=$(ss -H -tuln) || { status=$?; unset serve; printf 'ss -H -tuln failed (exit %s)\\n' \"$status\"; exit 1; }\n" +
		"printf '%s\\n' " + shell.Quote(tailscaleServeSectionMarker) + " \"$serve\" " + shell.Quote(listenersSectionMarker) + " \"$listeners\"\n" +
		"unset serve listeners"
}

// sysctlRuntimeCommand compares each value in the serverpro drop-in with the
// running kernel, and checks the drop-in still carries the serverpro values.
// The file only applies at boot, so a later writer (networkd, an agent, a
// hand-run sysctl -w) silently undoes it until the next reboot, and an edited
// file would undo it after that.
func sysctlRuntimeCommand() string {
	var expected strings.Builder
	for _, setting := range cloudinit.SysctlSettings() {
		expected.WriteString(" " + shell.Quote(setting.Key+"="+setting.Value))
	}
	return "export LC_ALL=C\n" +
		"conf=" + shell.Quote(cloudinit.SysctlConfigPath) + "\n" +
		"if [ ! -r \"$conf\" ]; then printf '" + driftWarnPrefix + "%s is missing\\n' \"$conf\"; exit 0; fi\n" +
		"normalize() { tr -s '[:space:]' ' ' | sed 's/^ //; s/ $//'; }\n" +
		"settings=$(awk -F= '/^[[:space:]]*([#;]|$)/ || NF < 2 { next } { key = $1; sub(/^[[:space:]-]+/, \"\", key); sub(/[[:space:]]+$/, \"\", key); value = substr($0, index($0, \"=\") + 1); gsub(/^[[:space:]]+|[[:space:]]+$/, \"\", value); print key \"=\" value }' \"$conf\")\n" +
		"for want in" + expected.String() + "; do\n" +
		"  printf '%s\\n' \"$settings\" | grep -Fxq \"$want\" || printf '" + driftWarnPrefix + "%s lacks serverpro setting %s\\n' \"$conf\" \"$want\"\n" +
		"done\n" +
		"printf '%s\\n' \"$settings\" | while IFS= read -r line; do\n" +
		"  [ -n \"$line\" ] || continue\n" +
		"  key=${line%%=*}\n" +
		"  want=$(printf '%s\\n' \"${line#*=}\" | normalize)\n" +
		"  have=$(sysctl -n \"$key\" 2>/dev/null | normalize) || have=\n" +
		"  [ \"$have\" = \"$want\" ] || printf '" + driftWarnPrefix + "%s is %s at runtime, %s in %s\\n' \"$key\" \"${have:-unreadable}\" \"$want\" \"$conf\"\n" +
		"done\n" +
		"printf 'compared %s\\n' \"$conf\""
}

// TailscaleServeIdleOutput is TailscaleServeCommand's answer on a host with no
// serve config and no listeners, which tailscale prints as an empty object.
func TailscaleServeIdleOutput() string {
	return tailscaleServeSectionMarker + "\n{}\n" + listenersSectionMarker + "\n"
}
