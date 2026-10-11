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
)

var sshdHardeningExpectations = map[string]string{
	sshdKeywordPermitRootLogin:              sshdValueDisabled,
	sshdKeywordPasswordAuthentication:       sshdValueDisabled,
	sshdKeywordKbdInteractiveAuthentication: sshdValueDisabled,
	sshdKeywordChallengeResponseAuth:        sshdValueDisabled,
	sshdKeywordX11Forwarding:                sshdValueDisabled,
	sshdKeywordAllowAgentForwarding:         sshdValueDisabled,
	sshdKeywordAllowTCPForwarding:           sshdValueDisabled,
	sshdKeywordPermitTunnel:                 sshdValueDisabled,
	sshdKeywordPermitOpen:                   sshdValueNone,
}

// sshdSettingValueCommand and sshdChallengeResponseCommand run inside the
// remote runner's root shell, so they call sshd and grep directly: a nested
// root-to-root sudo re-enters PAM account checks and fails on hosts whose
// root password is expired (some provider images), masking the real setting.
func sshdSettingValueCommand(keyword, value string) string {
	expected := strings.ToLower(keyword) + " " + value
	configured := keyword + " " + value
	return "out=\"$(sshd -T 2>&1)\"; " +
		"if printf '%s\n' \"$out\" | grep -Fx " + shell.Quote(expected) + "; then exit 0; fi; " +
		"case \"$out\" in *\"Missing privilege separation directory: /run/sshd\"*) " +
		"grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config[.]d/[*][.]conf([[:space:]]|$)' /etc/ssh/sshd_config && " +
		"grep -Fx " + shell.Quote(configured) + " /etc/ssh/sshd_config.d/99-serverpro.conf && " +
		"printf '%s\n' " + shell.Quote("sshd inactive; /run/sshd absent; serverpro config contains "+configured) + " ;; " +
		"*) printf '%s\n' \"$out\"; exit 1 ;; esac"
}

func sshdChallengeResponseCommand() string {
	configured := sshdKeywordChallengeResponseAuth + " " + sshdHardeningExpectations[sshdKeywordChallengeResponseAuth]
	effectiveExpected := strings.ToLower(sshdKeywordKbdInteractiveAuthentication) + " " + sshdHardeningExpectations[sshdKeywordKbdInteractiveAuthentication]
	return "out=\"$(sshd -T 2>&1)\"; " +
		"if printf '%s\n' \"$out\" | grep -Fx " + shell.Quote(effectiveExpected) + "; then exit 0; fi; " +
		"case \"$out\" in *\"Missing privilege separation directory: /run/sshd\"*) " +
		"grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config[.]d/[*][.]conf([[:space:]]|$)' /etc/ssh/sshd_config && " +
		"grep -Fx " + shell.Quote(configured) + " /etc/ssh/sshd_config.d/99-serverpro.conf && " +
		"printf '%s\n' " + shell.Quote("sshd inactive; /run/sshd absent; serverpro config contains "+configured) + " ;; " +
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
// it at most once per run in practice: later sshd checks re-read the host
// after it ran (refreshAfterSSHDRepair) instead of repairing again, because
// back-to-back restarts trip systemd's start rate limit.
func sshdSettingsFixCommand() string {
	return sshdSettingsFixScript("/etc/ssh", "/run/sshd")
}

// sshdSettingsFixScript validates before it commits and never leaves an
// invalid config on disk:
//   - it builds the candidate main config and serverpro drop-in in a private
//     temp directory and checks them with sshd -t -f against a copy of the
//     drop-in directory, so the live files stay untouched if that fails;
//   - it replaces each changed file through a same-directory temp file and
//     mv, then runs sshd -t on the live config and restores the backups if
//     that fails (for example when another drop-in changed meanwhile);
//   - every temp-file step aborts on failure.
//
// It then reloads with try-reload-or-restart: that reloads a running
// ssh.service (sshd supports reload, and reloads do not count towards the
// start limit) and does nothing while a socket-activated ssh.service is
// inactive, since the next socket-spawned sshd reads the new config. A
// running daemon is reloaded even when the files already match, because an
// earlier reload may have failed after its write and sshd -T only proves the
// on-disk config. With nothing changed and ssh.service inactive it only
// validates. sshDir and runDir are parameters so tests can run the script
// against a scratch tree; production passes /etc/ssh and /run/sshd.
func sshdSettingsFixScript(sshDir, runDir string) string {
	return `main=` + shell.Quote(sshDir+"/sshd_config") + `
dropin_dir=` + shell.Quote(sshDir+"/sshd_config.d") + `
dropin="$dropin_dir/99-serverpro.conf"
mkdir -p "$dropin_dir" ` + shell.Quote(runDir) + ` || exit 1
work="$(mktemp -d)" || { echo 'sshd repair: cannot create a temp directory; live config unchanged' >&2; exit 1; }
trap 'rm -rf "$work"' EXIT
cat > "$work/99-serverpro.conf" <<'EOF' || exit 1
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding no
PermitTunnel no
PermitOpen none
EOF
main_changed=0
dropin_changed=0
if grep -Eq '^[[:space:]]*Include[[:space:]]+'"$dropin_dir"'/[*][.]conf([[:space:]]|$)' "$main"; then
  cp "$main" "$work/main" || exit 1
else
  { printf 'Include %s/*.conf\n' "$dropin_dir"; cat "$main"; } > "$work/main" || exit 1
  main_changed=1
fi
cmp -s "$work/99-serverpro.conf" "$dropin" || dropin_changed=1
changed=0
if [ "$main_changed" -eq 1 ] || [ "$dropin_changed" -eq 1 ]; then
  changed=1
  mkdir "$work/d" || exit 1
  for conf in "$dropin_dir"/*.conf; do
    [ -e "$conf" ] || continue
    cp "$conf" "$work/d/" || exit 1
  done
  cp "$work/99-serverpro.conf" "$work/d/99-serverpro.conf" || exit 1
  sed "s#^\([[:space:]]*Include[[:space:]]\{1,\}\)$dropin_dir/[*][.]conf#\1$work/d/*.conf#" "$work/main" > "$work/candidate" || exit 1
  if ! sshd -t -f "$work/candidate"; then
    echo 'sshd repair: candidate config failed sshd -t; live config unchanged' >&2
    exit 1
  fi
  cp -p "$main" "$work/main.bak" || exit 1
  had_dropin=0
  if [ -e "$dropin" ]; then cp -p "$dropin" "$work/dropin.bak" || exit 1; had_dropin=1; fi
  restore() {
    cp -p "$work/main.bak" "$main.serverpro-restore" && mv -f "$main.serverpro-restore" "$main"
    if [ "$had_dropin" -eq 1 ]; then
      cp -p "$work/dropin.bak" "$dropin.serverpro-restore" && mv -f "$dropin.serverpro-restore" "$dropin"
    else
      rm -f "$dropin"
    fi
  }
  commit() {
    cp "$1" "$2.serverpro-new" && chmod 0644 "$2.serverpro-new" && mv -f "$2.serverpro-new" "$2"
  }
  if [ "$main_changed" -eq 1 ]; then
    commit "$work/main" "$main" || { rm -f "$main.serverpro-new"; restore; exit 1; }
  fi
  if [ "$dropin_changed" -eq 1 ]; then
    commit "$work/99-serverpro.conf" "$dropin" || { rm -f "$dropin.serverpro-new"; restore; exit 1; }
  fi
  if ! sshd -t; then
    restore
    echo 'sshd repair: live config failed sshd -t after the write; restored the previous files' >&2
    exit 1
  fi
fi
if [ "$changed" -eq 0 ] && ! systemctl is-active --quiet ` + sshServiceUnit + `; then sshd -t; exit; fi
sshd -t && systemctl try-reload-or-restart ssh`
}
