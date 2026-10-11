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

// sshdUnitsReadCommand fails while either ssh unit sits in the failed state,
// naming the units so the evidence points at the exact recovery target.
func sshdUnitsReadCommand() string {
	return "failed=''\n" +
		"for unit in " + sshSocketUnit + " " + sshServiceUnit + "; do\n" +
		"  if systemctl is-failed --quiet \"$unit\"; then failed=\"$failed $unit\"; fi\n" +
		"done\n" +
		"if [ -n \"$failed\" ]; then printf 'failed ssh units:%s\\n' \"$failed\"; exit 1; fi\n" +
		"printf '%s\\n' 'no failed ssh units'"
}

// sshdUnitsFixCommand clears the failed state and restores whichever unit
// the host enables, so a socket-activated host gets its listener back
// without also starting a standalone daemon. It never enables a disabled
// unit; public port 22 stays closed by the ufw checks either way.
func sshdUnitsFixCommand() string {
	return "for unit in " + sshSocketUnit + " " + sshServiceUnit + "; do\n" +
		"  if systemctl is-failed --quiet \"$unit\"; then systemctl reset-failed \"$unit\"; fi\n" +
		"done\n" +
		"if systemctl is-enabled --quiet " + sshSocketUnit + "; then systemctl start " + sshSocketUnit + "\n" +
		"elif systemctl is-enabled --quiet " + sshServiceUnit + "; then systemctl start " + sshServiceUnit + "\n" +
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

// sshdSettingsFixCommand is shared by every sshd hardening check, so one
// doctor --fix run can invoke it once per failing setting. It therefore only
// rewrites and reloads when the managed config actually differs: back-to-back
// restarts trip systemd's start rate limit and leave ssh.service failed.
// try-reload-or-restart applies a change to a running daemon without a new
// start and leaves a socket-activated, inactive ssh.service inactive; the next
// socket-spawned sshd reads the new config.
func sshdSettingsFixCommand() string {
	return `mkdir -p /etc/ssh/sshd_config.d /run/sshd
changed=0
if ! grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config[.]d/[*][.]conf([[:space:]]|$)' /etc/ssh/sshd_config; then
  tmp="$(mktemp)"
  printf 'Include /etc/ssh/sshd_config.d/*.conf\n' > "$tmp"
  cat /etc/ssh/sshd_config >> "$tmp"
  cat "$tmp" > /etc/ssh/sshd_config
  rm -f "$tmp"
  changed=1
fi
desired="$(mktemp)"
cat > "$desired" <<'EOF'
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
if ! cmp -s "$desired" /etc/ssh/sshd_config.d/99-serverpro.conf; then
  cat "$desired" > /etc/ssh/sshd_config.d/99-serverpro.conf
  changed=1
fi
rm -f "$desired"
if [ "$changed" -eq 0 ]; then sshd -t; exit; fi
sshd -t && systemctl try-reload-or-restart ssh`
}
