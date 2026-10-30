package doctor

import (
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/cloudinit"
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
	egressRestrictedCheckName = "egress restricted"
	// The blocked-egress probe connects to DNS over TLS on a public resolver:
	// the port is always served there, so a connect proves the path is open,
	// and it is not on serverpro's allow-out list (internal/network), so
	// restricted egress must drop it.
	egressBlockedProbeHost        = "1.1.1.1"
	egressBlockedProbePort        = 853
	egressBlockedProbeTimeoutSecs = 3
	egressRestrictedRemediation   = "restricted egress is not in force: confirm no workload needs other outbound ports, then apply the serverpro egress lockdown (USAGE.md, Restricted egress) and rerun doctor"

	rebootPendingCheckName = "reboot pending"
	rebootRequiredPath     = "/var/run/reboot-required"
	rebootRequiredPkgsPath = rebootRequiredPath + ".pkgs"
	// kernelImagePrefix is where Ubuntu installs one image per kernel
	// package; the bare /boot/vmlinuz symlinks do not match the prefix.
	kernelImagePrefix        = "/boot/vmlinuz-"
	rebootPendingRemediation = "security updates wait for a reboot: schedule one (sudo systemctl reboot) when the workload allows, then rerun doctor"

	privilegedAccountsCheckName   = "privileged accounts"
	privilegedAccountsRemediation = "remove each listed account from the group (gpasswd -d USER GROUP), or delete an unused provider image account (userdel -r USER), after confirming nothing runs as it"

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

	sysctlRuntimeCheckName   = "sysctl runtime"
	sysctlRuntimeRemediation = "re-apply the file (sudo sysctl -p " + cloudinit.SysctlConfigPath + "); if the value drifts again, find the later writer (systemd-sysctl order, networkd, provider agents) before the next reboot"
)

// privilegedGroups grant root or a root-equivalent: sudo and admin through
// Ubuntu's sudoers, lxd and docker through privileged containers.
var privilegedGroups = []string{"sudo", "admin", "lxd", "docker"}

// egressRestrictedCommand checks the policy and the effect separately: UFW
// can report "deny (outgoing)" while an ALLOW OUT Anywhere rule, a second
// firewall, or a missed reload still lets traffic out, so a real connect to a
// port the lockdown never allows is the proof.
func egressRestrictedCommand() string {
	target := egressBlockedProbeHost + ":" + strconv.Itoa(egressBlockedProbePort)
	return "export LC_ALL=C\n" +
		"status=$(ufw status verbose) || exit 1\n" +
		"outgoing=$(printf '%s\\n' \"$status\" | sed -n 's/^Default:.*[[:space:]]\\([a-z]*\\) (outgoing).*/\\1/p')\n" +
		"case \"$outgoing\" in\n" +
		"  deny|reject) printf 'ufw default outgoing: %s\\n' \"$outgoing\" ;;\n" +
		"  *) printf '" + driftFailPrefix + "ufw default outgoing is %s, want deny\\n' \"${outgoing:-unset (ufw inactive?)}\" ;;\n" +
		"esac\n" +
		"if timeout " + strconv.Itoa(egressBlockedProbeTimeoutSecs) + " bash -c " + shell.Quote("exec 3<>/dev/tcp/"+egressBlockedProbeHost+"/"+strconv.Itoa(egressBlockedProbePort)) + " 2>/dev/null; then\n" +
		"  printf '%s\\n' " + shell.Quote(driftFailPrefix+"tcp "+target+" connected; non-allowlisted egress is open") + "\n" +
		"else\n" +
		"  printf '%s\\n' " + shell.Quote("tcp "+target+" blocked") + "\n" +
		"fi"
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
// that holds root through a group or uid 0. It counts primary-group members
// too, because getent group lists only supplementary ones, and provider
// images add their default user (for example linuxuser) this way or the other.
func privilegedAccountsCommand(admin string) string {
	return "export LC_ALL=C\n" +
		"admin=" + shell.Quote(admin) + "\n" +
		"{\n" +
		"for group in " + strings.Join(privilegedGroups, " ") + "; do\n" +
		"  entry=$(getent group \"$group\") || continue\n" +
		"  gid=$(printf '%s\\n' \"$entry\" | cut -d: -f3)\n" +
		"  for member in $(printf '%s\\n' \"$entry\" | cut -d: -f4 | tr ',' ' ') $(getent passwd | awk -F: -v gid=\"$gid\" '$4 == gid { print $1 }'); do\n" +
		"    [ \"$member\" = \"$admin\" ] && continue\n" +
		"    printf '" + driftFailPrefix + "%s is in privileged group %s\\n' \"$member\" \"$group\"\n" +
		"  done\n" +
		"done\n" +
		"getent passwd | awk -F: '$3 == 0 && $1 != \"root\" { printf \"" + driftFailPrefix + "%s has uid 0\\n\", $1 }'\n" +
		"} | sort -u\n" +
		"printf 'checked groups: %s\\n' " + shell.Quote(strings.Join(privilegedGroups, " "))
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
// It is exported, with TailscaleServeIdleOutput, for remote fakes that answer
// by script: the probe parses its output, so a generic success string fails.
func TailscaleServeCommand() string {
	return "export LC_ALL=C\n" +
		"printf '%s\\n' " + shell.Quote(tailscaleServeSectionMarker) + "\n" +
		"tailscale serve status --json || exit 1\n" +
		"printf '%s\\n' " + shell.Quote(listenersSectionMarker) + "\n" +
		"ss -H -tuln || exit 1"
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
