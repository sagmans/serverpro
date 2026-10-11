package doctor

import (
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	sshdValueDisabled = "no"
	sshdValueNone     = "none"

	// DNS canary isolates resolver failure from egress failure: quad100 upstream
	// loss (2026-07 incident) presents as DNS-only breakage with healthy TCP
	// egress, and the bundled "egress positive" probe misattributes it.
	dnsCanaryName            = "one.one.one.one"
	dnsResolutionRemediation = "check tailnet DNS global nameservers (admin console → DNS) and host resolver (tailscale dns status)"

	// Egress targets: one name-resolved site and one literal IP, so a failure
	// separates resolver trouble from blocked outbound TLS.
	egressResolveName         = "ubuntu.com"
	egressNameTarget          = "https://" + egressResolveName
	egressAddressTarget       = "https://1.1.1.1"
	egressProbeTimeoutSecs    = 10
	egressNoResponseCode      = "000"
	egressPositiveRemediation = "check provider firewall outbound rules, ufw egress rules, and the host route (ip route; tailscale status)"

	sshdKeywordPermitRootLogin              = "PermitRootLogin"
	sshdKeywordPasswordAuthentication       = "PasswordAuthentication"
	sshdKeywordKbdInteractiveAuthentication = "KbdInteractiveAuthentication"
	sshdKeywordChallengeResponseAuth        = "ChallengeResponseAuthentication"
	sshdKeywordX11Forwarding                = "X11Forwarding"
	sshdKeywordAllowAgentForwarding         = "AllowAgentForwarding"
	sshdKeywordAllowTCPForwarding           = "AllowTcpForwarding"
	sshdKeywordPermitTunnel                 = "PermitTunnel"
	sshdKeywordPermitOpen                   = "PermitOpen"

	// sshd units check: a failed ssh unit is invisible to the sshd -T
	// setting checks, and earlier repairs that restarted ssh once per failing
	// setting left both units failed on start-limit-hit.
	sshdUnitsCheckName = "sshd units"
	sshSocketUnit      = "ssh.socket"
	sshServiceUnit     = "ssh.service"

	// sshd file layout on the managed host. The setting reads and the repair
	// share it so they always judge and write the same files; the repair takes
	// the directories as parameters so tests can point it at a scratch tree.
	sshdConfigDir         = "/etc/ssh"
	sshdPrivsepDir        = "/run/sshd"
	sshdMainConfigName    = "sshd_config"
	sshdDropinDirName     = "sshd_config.d"
	sshdManagedDropinName = "99-serverpro.conf"
	// sshdDropinGlob is the Include argument stock Ubuntu ships, relative to
	// sshdConfigDir as sshd resolves it.
	sshdDropinGlob = sshdDropinDirName + "/*.conf"
	// sshd refuses to start when this directory is absent, which is the
	// normal state of a socket-activated host before its first connection.
	sshdMissingPrivsepMessage = "Missing privilege separation directory: " + sshdPrivsepDir
	sshdManagedFileMode       = "0644"
	// Same-directory temp names make each replace a rename, so sshd never
	// reads a half-written file; cleanup removes them on any exit.
	sshdStageSuffix   = ".serverpro-new"
	sshdRestoreSuffix = ".serverpro-restore"
	// sshdRestoreFailedExit sets a broken rollback apart from an ordinary
	// failed repair, because only the former can lock out new SSH logins.
	sshdRestoreFailedExit = 3
)

// sshdHardening is the serverpro drop-in in file order. The checks look
// settings up through sshdHardeningExpectations; the repair writes this list
// and then confirms each value through sshd -T.
var sshdHardening = []struct{ keyword, value string }{
	{sshdKeywordPermitRootLogin, sshdValueDisabled},
	{sshdKeywordPasswordAuthentication, sshdValueDisabled},
	{sshdKeywordKbdInteractiveAuthentication, sshdValueDisabled},
	{sshdKeywordChallengeResponseAuth, sshdValueDisabled},
	{sshdKeywordX11Forwarding, sshdValueDisabled},
	{sshdKeywordAllowAgentForwarding, sshdValueDisabled},
	{sshdKeywordAllowTCPForwarding, sshdValueDisabled},
	{sshdKeywordPermitTunnel, sshdValueDisabled},
	{sshdKeywordPermitOpen, sshdValueNone},
}

var sshdHardeningExpectations = func() map[string]string {
	expectations := make(map[string]string, len(sshdHardening))
	for _, setting := range sshdHardening {
		expectations[setting.keyword] = setting.value
	}
	return expectations
}()

// sshdEffectiveLine is the sshd -T line that proves a hardening setting took
// effect. ChallengeResponseAuthentication is a deprecated alias that sshd -T
// never prints, so it is proven through KbdInteractiveAuthentication.
func sshdEffectiveLine(keyword string) string {
	if keyword == sshdKeywordChallengeResponseAuth {
		keyword = sshdKeywordKbdInteractiveAuthentication
	}
	return strings.ToLower(keyword) + " " + sshdHardeningExpectations[keyword]
}

// sshdTopLevelIncludeAwk exits 0 only when the main config includes the
// drop-in directory before its first Match block. An Include inside a Match
// block applies the drop-ins to that block alone, so sshd -T would not show
// them and the repair could never settle the checks. It accepts the absolute
// and the relative (sshdConfigDir-based) spelling, and compares keywords
// case-insensitively as sshd does.
const sshdTopLevelIncludeAwk = `tolower($1) == "match" { exit }
tolower($1) == "include" { for (i = 2; i <= NF; i++) if ($i == abs || $i == rel) { found = 1; exit } }
END { exit !found }`

// sshdIncludeRewriteAwk points every Include of the drop-in directory at the
// private copy, so the candidate is validated against the new drop-in instead
// of whatever the live directory holds.
const sshdIncludeRewriteAwk = `tolower($1) == "include" { for (i = 2; i <= NF; i++) if ($i == abs || $i == rel) $i = to }
{ print }`

// sshdTopLevelIncludeTest is the shell test the setting reads share with the
// repair, so a Match-scoped Include fails the read fallback too.
func sshdTopLevelIncludeTest(sshDir string) string {
	return "awk -v abs=" + shell.Quote(sshDir+"/"+sshdDropinGlob) + " -v rel=" + shell.Quote(sshdDropinGlob) + " " +
		shell.Quote(sshdTopLevelIncludeAwk) + " " + shell.Quote(sshDir+"/"+sshdMainConfigName)
}

// sshdSettingValueCommand and sshdChallengeResponseCommand run inside the
// remote runner's root shell, so they call sshd and grep directly: a nested
// root-to-root sudo re-enters PAM account checks and fails on hosts whose
// root password is expired (some provider images), masking the real setting.
func sshdSettingValueCommand(keyword, value string) string {
	return sshdSettingReadCommand(strings.ToLower(keyword)+" "+value, keyword+" "+value)
}

func sshdChallengeResponseCommand() string {
	return sshdSettingReadCommand(sshdEffectiveLine(sshdKeywordChallengeResponseAuth),
		sshdKeywordChallengeResponseAuth+" "+sshdHardeningExpectations[sshdKeywordChallengeResponseAuth])
}

// sshdSettingReadCommand trusts sshd -T first. Only when sshd cannot run
// because its privilege separation directory is missing (an inactive
// socket-activated host) does it fall back to the files the repair writes.
func sshdSettingReadCommand(effective, configured string) string {
	return "out=\"$(sshd -T 2>&1)\"; " +
		"if printf '%s\n' \"$out\" | grep -Fx " + shell.Quote(effective) + "; then exit 0; fi; " +
		"case \"$out\" in *" + shell.Quote(sshdMissingPrivsepMessage) + "*) " +
		sshdTopLevelIncludeTest(sshdConfigDir) + " && " +
		"grep -Fx " + shell.Quote(configured) + " " + shell.Quote(sshdConfigDir+"/"+sshdDropinDirName+"/"+sshdManagedDropinName) + " && " +
		"printf '%s\n' " + shell.Quote("sshd inactive; "+sshdPrivsepDir+" absent; serverpro config contains "+configured) + " ;; " +
		"*) printf '%s\n' \"$out\"; exit 1 ;; esac"
}

// sshdUnitsReadCommand fails while an ssh unit is failed, or enabled but not
// running, and names each unit so the evidence points at the recovery target.
// ssh.service stays inactive by design while ssh.socket listens, so that case
// passes.
func sshdUnitsReadCommand() string {
	return "bad=''\n" +
		"for unit in " + sshSocketUnit + " " + sshServiceUnit + "; do\n" +
		"  if systemctl is-failed --quiet \"$unit\"; then bad=\"$bad $unit:failed\"; continue; fi\n" +
		"  if [ \"$unit\" = " + sshServiceUnit + " ] && systemctl is-active --quiet " + sshSocketUnit + "; then continue; fi\n" +
		"  if systemctl is-enabled --quiet \"$unit\" && ! systemctl is-active --quiet \"$unit\"; then bad=\"$bad $unit:inactive\"; fi\n" +
		"done\n" +
		"if [ -n \"$bad\" ]; then printf 'unhealthy ssh units:%s\\n' \"$bad\"; exit 1; fi\n" +
		"printf '%s\\n' 'ssh units healthy'"
}

// sshdUnitsFixCommand clears the failed state and starts, unless it already
// runs, whichever unit the host enables, preferring the socket so a socket-activated host does not
// also gain a standalone daemon. It never enables a disabled unit, so it only
// returns the host to its own configured listener; the ufw checks earlier in
// the same plan are what keep public port 22 denied.
func sshdUnitsFixCommand() string {
	return "for unit in " + sshSocketUnit + " " + sshServiceUnit + "; do\n" +
		"  if systemctl is-failed --quiet \"$unit\"; then systemctl reset-failed \"$unit\"; fi\n" +
		"done\n" +
		"if systemctl is-enabled --quiet " + sshSocketUnit + "; then systemctl is-active --quiet " + sshSocketUnit + " || systemctl start " + sshSocketUnit + "\n" +
		"elif systemctl is-enabled --quiet " + sshServiceUnit + "; then systemctl is-active --quiet " + sshServiceUnit + " || systemctl start " + sshServiceUnit + "\n" +
		"fi"
}

const gitSigningPublicKeyRelativePath = ".ssh/id_ed25519_sign.pub"

// gitUserConfigCommand resolves the admin home once because repeated command
// fragments previously let shell list separators mask earlier failures.
func gitUserConfigCommand(user, body string) string {
	quotedUser := shell.Quote(user)
	return "set -eu\n" +
		"home=\"$(getent passwd " + quotedUser + " | cut -d: -f6)\"\n" +
		"test -n \"$home\"\n" +
		"git_config() { runuser -u " + quotedUser + " -- env HOME=\"$home\" git config --global \"$@\"; }\n" +
		body
}

func gitIdentityReadCommand(user string, identity config.GitIdentity) string {
	return gitUserConfigCommand(user,
		"test \"$(git_config --get user.name)\" = "+shell.Quote(identity.Name)+"\n"+
			"test \"$(git_config --get user.email)\" = "+shell.Quote(identity.Email))
}

func gitIdentityFixCommand(user string, identity config.GitIdentity) string {
	return gitUserConfigCommand(user,
		"git_config user.name "+shell.Quote(identity.Name)+"\n"+
			"git_config user.email "+shell.Quote(identity.Email))
}

func gitSigningReadCommand(user string) string {
	return gitUserConfigCommand(user,
		"test \"$(git_config --get gpg.format)\" = ssh\n"+
			"test \"$(git_config --get user.signingkey)\" = \"$home/"+gitSigningPublicKeyRelativePath+"\"\n"+
			"test \"$(git_config --get commit.gpgsign)\" = true")
}

func gitSigningFixCommand(user string) string {
	return gitUserConfigCommand(user,
		"git_config gpg.format ssh\n"+
			"git_config user.signingkey \"$home/"+gitSigningPublicKeyRelativePath+"\"\n"+
			"git_config commit.gpgsign true")
}

func githubSSHAuthReadCommand(user string) string {
	quotedUser := shell.Quote(user)
	return "home=\"$(getent passwd " + quotedUser + " | cut -d: -f6)\"; " +
		"out=\"$(runuser -u " + quotedUser + " -- env HOME=\"$home\" ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -T git@github.com 2>&1)\"; " +
		"printf '%s\n' \"$out\"; case \"$out\" in *'successfully authenticated'*) exit 0 ;; esac; exit 1"
}

const (
	ghHostsConfigRelativePath = ".config/gh/hosts.yml"
	ghTokenFingerprintLength  = 12
	// A credential probe must answer quickly or not at all: an unanswered probe
	// stays unknown instead of being read as a dead credential.
	ghTokenProbeConnectTimeoutSeconds = 5
	ghTokenProbeMaxTimeSeconds        = 20
)

// ghCredentialReadCommand reports a truncated SHA-256 of the remote gh token
// plus GitHub's own verdict on that token, never the credential itself, so
// parity evidence stays safe in logs and failure artifacts even though the
// token sits in hosts.yml. WHY GitHub instead of the local gh CLI: a missing,
// broken, or not-yet-installed mise/gh toolchain fails exactly like a rejected
// credential, and a verdict that cannot tell the two apart used to authorise
// replacing a working remote token during --fix. The header travels on stdin
// so the probe never publishes the credential to local process arguments.
func ghCredentialReadCommand(user string) string {
	quotedUser := shell.Quote(user)
	return "set -eu\n" +
		lifecycle.GHAuthorizationHeaderScript() +
		"home=\"$(getent passwd " + quotedUser + " | cut -d: -f6)\"\n" +
		"hosts=\"$home/" + ghHostsConfigRelativePath + "\"\n" +
		"token=\"\"\n" +
		"if [ -r \"$hosts\" ]; then\n" +
		"  token=\"$(awk '/^github\\.com:/{f=1;next} f && /^[[:space:]]*oauth_token:/{sub(/^[[:space:]]*oauth_token:[[:space:]]*/,\"\"); print; exit}' \"$hosts\" 2>/dev/null || true)\"\n" +
		"fi\n" +
		"if [ -z \"$token\" ]; then printf 'sha=absent\\nauth=absent\\n'; exit 0; fi\n" +
		"printf 'sha=%s\\n' \"$(printf '%s' \"$token\" | sha256sum | cut -c1-" + strconv.Itoa(ghTokenFingerprintLength) + ")\"\n" +
		`case "$token" in` + "\n" +
		`  *'"'*|*'\'*) printf 'auth=unknown\n'; exit 0 ;;` + "\n" +
		"esac\n" +
		`status="$(gh_authorization_header "$token" | curl -sS -K - -o /dev/null -w '%{http_code}' --connect-timeout ` + strconv.Itoa(ghTokenProbeConnectTimeoutSeconds) + ` --max-time ` + strconv.Itoa(ghTokenProbeMaxTimeSeconds) + ` https://api.github.com/user 2>/dev/null || true)"` + "\n" +
		"case \"$status\" in\n" +
		"  200) printf 'auth=ok\\n' ;;\n" +
		"  401) printf 'auth=failed\\n' ;;\n" +
		"  *) printf 'auth=unknown\\n' ;;\n" +
		"esac"
}

func ghAuthReadCommand(user string) string {
	quotedUser := shell.Quote(user)
	return "home=\"$(getent passwd " + quotedUser + " | cut -d: -f6)\"; " +
		"runuser -u " + quotedUser + " -- env HOME=\"$home\" \"$home/.local/bin/mise\" exec -- gh auth status"
}

func dnsResolutionCommand() string {
	return "getent hosts " + dnsCanaryName + " >/dev/null && echo resolved || { echo 'dns resolution failed for " + dnsCanaryName + "'; exit 1; }"
}

// egressPositiveCommand accepts any HTTP status as proof of egress. A site may
// answer 403 or 429 to datacenter ranges (seen live from a cloud datacenter
// after a power cycle), and that is the site's policy, not a broken outbound
// path. Only no response at all (curl code 000) fails, and the output names
// the target so the evidence says which leg broke.
func egressPositiveCommand() string {
	probe := func(target string) string {
		return "code=\"$(curl -sS -o /dev/null -I -m " + strconv.Itoa(egressProbeTimeoutSecs) + " -w '%{http_code}' " + shell.Quote(target) + " 2>&1)\"; " +
			"case \"$code\" in " + egressNoResponseCode + "*|*[!0-9]*|'') echo " + shell.Quote("no response from "+target+": ") + "\"$code\"; exit 1 ;; esac; " +
			"echo " + shell.Quote(target+" http ") + "\"$code\"; "
	}
	return "getent hosts " + egressResolveName + " >/dev/null || { echo " + shell.Quote("dns resolution failed for "+egressResolveName) + "; exit 1; }; " +
		probe(egressNameTarget) + probe(egressAddressTarget)
}

func ufwSSHIngressCommand() string {
	return "out=\"$(ufw status numbered verbose)\"; " +
		"if printf '%s\n' \"$out\" | grep -Ei '(^|[[:space:]])(22|OpenSSH|ssh)(/tcp)?[[:space:]].*ALLOW IN'; then printf '%s\n' \"$out\"; exit 1; fi; " +
		"printf '%s\n' 'no SSH ALLOW IN rules'"
}

func sudoPasswordRequiredCommand(user string) string {
	quotedUser := shell.Quote(user)
	return "runuser -u " + quotedUser + " -- sudo -k || true\n" +
		"if runuser -u " + quotedUser + " -- sudo -n true >/dev/null 2>&1; then\n" +
		"  printf '%s\n' 'admin sudo permits NOPASSWD:ALL'; exit 1\n" +
		"fi\n" +
		"printf '%s\n' 'admin sudo requires password'"
}

// sudoPasswordFixInput and sudoPasswordFixCommand reuse the provision script,
// so doctor --fix repairs a host through the same SSH path that set it up.
func sudoPasswordFixInput(password string) string {
	return lifecycle.AdminPasswordInput(password)
}

func sudoPasswordFixCommand(user string) string {
	return lifecycle.AdminPasswordScript(user)
}

// adminPasswordHashCommand reports only the crypt scheme of the admin shadow
// entry, never the hash. SHA-crypt and older schemes fail so doctor --fix can
// re-set the password and let the host hash it with its default (yescrypt).
func adminPasswordHashCommand(user string) string {
	return `entry="$(getent shadow ` + shell.Quote(user) + `)" || { echo 'admin user has no shadow entry'; exit 1; }
hash="$(printf '%s' "$entry" | cut -d: -f2)"
case "$hash" in
  '$y$'*|'$gy$'*|'$7$'*|'$2b$'*|'$2y$'*) printf 'admin password uses crypt scheme $%s$\n' "$(printf '%s' "$hash" | cut -d'$' -f2)" ;;
  ''|'!'*|'*'*) echo 'admin password is locked or not set'; exit 1 ;;
  '$'*) printf 'admin password uses legacy crypt scheme $%s$\n' "$(printf '%s' "$hash" | cut -d'$' -f2)"; exit 1 ;;
  *) echo 'admin password uses legacy DES crypt'; exit 1 ;;
esac`
}

// sshdSettingsFixCommand is shared by every sshd hardening check. Doctor runs
// it at most once per run: later sshd checks re-read the host after it ran
// (refreshAfterSSHDRepair), and a check it did not settle fails instead of
// running it again (remoteFixableCheck), because a second run of the same
// script cannot change the outcome and only reloads sshd once more.
func sshdSettingsFixCommand() string {
	return sshdSettingsFixScript(sshdConfigDir, sshdPrivsepDir)
}

// sshdManagedDropin is the serverpro drop-in body. The repair writes it and
// compares it byte for byte, so a rerun on a settled host rewrites nothing.
func sshdManagedDropin() string {
	var body strings.Builder
	for _, setting := range sshdHardening {
		body.WriteString(setting.keyword + " " + setting.value + "\n")
	}
	return body.String()
}

// sshdSettingsFixScript keeps the host's SSH login path working while it
// changes sshd's config, since a broken config locks out every new session:
//   - it builds the candidate main config and drop-in in a private temp
//     directory and validates them with sshd -t -f against a copy of the
//     drop-in directory, so a rejected candidate never touches the live files;
//   - it writes the drop-in before the main config. Until the main config
//     includes the drop-in directory the new drop-in is inert, so a repair
//     killed between the two writes leaves either the old config or the
//     validated one, never an unvalidated mix;
//   - each write goes through a same-directory temp file and mv. After them it
//     runs sshd -t on the live config and restores the backups if that fails
//     (for example when another drop-in changed meanwhile). A failed rollback
//     exits with sshdRestoreFailedExit and names the files to fix by hand,
//     because only an operator still logged in can repair them then;
//   - the EXIT trap removes temp files. dash skips EXIT traps on fatal
//     signals, so the signal traps exit explicitly to run it, for example when
//     the SSH session drops at the doctor deadline.
//
// It then reloads with try-reload-or-restart: that reloads a running
// ssh.service (sshd supports reload, and reloads do not count towards the
// start limit) and does nothing while a socket-activated ssh.service is
// inactive, since the next socket-spawned sshd reads the new config. A
// running daemon is reloaded even when the files already match, because an
// earlier reload may have failed after its write and sshd -T only proves the
// on-disk config. Last, it checks every managed value through sshd -T: a
// valid config can still hide the drop-in (a lower-numbered drop-in sets the
// keyword first, or the Include sits inside a Match block), and reporting
// success then would leave every sshd check failing with no explanation.
func sshdSettingsFixScript(sshDir, runDir string) string {
	var expected strings.Builder
	for _, setting := range sshdHardening {
		expected.WriteString(" " + shell.Quote(sshdEffectiveLine(setting.keyword)))
	}
	dropinDir := sshDir + "/" + sshdDropinDirName
	return `main=` + shell.Quote(sshDir+"/"+sshdMainConfigName) + `
dropin_dir=` + shell.Quote(dropinDir) + `
dropin=` + shell.Quote(dropinDir+"/"+sshdManagedDropinName) + `
abs_include=` + shell.Quote(dropinDir+"/*.conf") + `
rel_include=` + shell.Quote(sshdDropinGlob) + `
stage=` + shell.Quote(sshdStageSuffix) + `
restore_suffix=` + shell.Quote(sshdRestoreSuffix) + `
work=
cleanup() {
  if [ -n "$work" ]; then rm -rf "$work"; fi
  rm -f "$main$stage" "$dropin$stage" "$main$restore_suffix" "$dropin$restore_suffix"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM PIPE
mkdir -p "$dropin_dir" ` + shell.Quote(runDir) + ` || exit 1
work="$(mktemp -d)" || { echo 'sshd repair: cannot create a temp directory; live config unchanged' >&2; exit 1; }
cat > "$work/managed" <<'EOF' || exit 1
` + sshdManagedDropin() + `EOF
main_changed=0
dropin_changed=0
if awk -v abs="$abs_include" -v rel="$rel_include" ` + shell.Quote(sshdTopLevelIncludeAwk) + ` "$main"; then
  cp "$main" "$work/main" || exit 1
else
  { printf 'Include %s\n' "$abs_include"; cat "$main"; } > "$work/main" || exit 1
  main_changed=1
fi
cmp -s "$work/managed" "$dropin" || dropin_changed=1
changed=0
if [ "$main_changed" -eq 1 ] || [ "$dropin_changed" -eq 1 ]; then
  changed=1
  mkdir "$work/d" || exit 1
  for conf in "$dropin_dir"/*.conf; do
    [ -e "$conf" ] || continue
    cp "$conf" "$work/d/" || exit 1
  done
  cp "$work/managed" "$work/d/` + sshdManagedDropinName + `" || exit 1
  awk -v abs="$abs_include" -v rel="$rel_include" -v to="$work/d/*.conf" ` + shell.Quote(sshdIncludeRewriteAwk) + ` "$work/main" > "$work/candidate" || exit 1
  if ! sshd -t -f "$work/candidate"; then
    echo 'sshd repair: candidate config failed sshd -t; live config unchanged' >&2
    exit 1
  fi
  cp -p "$main" "$work/main.bak" || exit 1
  had_dropin=0
  if [ -e "$dropin" ]; then cp -p "$dropin" "$work/dropin.bak" || exit 1; had_dropin=1; fi
  commit() {
    cp "$1" "$2$stage" && chmod ` + sshdManagedFileMode + ` "$2$stage" && mv -f "$2$stage" "$2"
  }
  put_back() {
    cp -p "$1" "$2$restore_suffix" && mv -f "$2$restore_suffix" "$2"
  }
  restore() {
    rc=0
    put_back "$work/main.bak" "$main" || rc=1
    if [ "$had_dropin" -eq 1 ]; then
      put_back "$work/dropin.bak" "$dropin" || rc=1
    else
      rm -f "$dropin" || rc=1
    fi
    return "$rc"
  }
  abort() {
    if ! restore; then
      printf 'sshd repair: %s; restoring the previous files FAILED: check %s and %s with sshd -t before closing this session\n' "$1" "$main" "$dropin" >&2
      exit ` + strconv.Itoa(sshdRestoreFailedExit) + `
    fi
    if ! sshd -t; then
      printf 'sshd repair: %s; restored the previous files, but they fail sshd -t too: check %s and %s before closing this session\n' "$1" "$main" "$dropin" >&2
      exit ` + strconv.Itoa(sshdRestoreFailedExit) + `
    fi
    printf 'sshd repair: %s; restored the previous files\n' "$1" >&2
    exit 1
  }
  if [ "$dropin_changed" -eq 1 ]; then
    commit "$work/managed" "$dropin" || abort 'cannot write the serverpro drop-in'
  fi
  if [ "$main_changed" -eq 1 ]; then
    commit "$work/main" "$main" || abort 'cannot write the main config'
  fi
  sshd -t || abort 'live config failed sshd -t after the write'
fi
if [ "$changed" -eq 1 ] || systemctl is-active --quiet ` + sshServiceUnit + `; then
  sshd -t && systemctl try-reload-or-restart ` + sshServiceUnit + ` || exit 1
else
  sshd -t || exit 1
fi
effective="$(sshd -T 2>&1)" || { printf '%s\n' "$effective" >&2; exit 1; }
missing=
for want in` + expected.String() + `; do
  printf '%s\n' "$effective" | grep -Fxq "$want" || missing="$missing [$want]"
done
if [ -n "$missing" ]; then
  printf 'sshd repair: config is valid but sshd -T lacks%s: a lower-numbered drop-in or an Include inside a Match block overrides %s\n' "$missing" "$dropin" >&2
  exit 1
fi`
}
