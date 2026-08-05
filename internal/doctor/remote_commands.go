package doctor

import (
	"strings"

	"github.com/sagmans/serverpro/internal/config"
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

	sshdKeywordPermitRootLogin              = "PermitRootLogin"
	sshdKeywordPasswordAuthentication       = "PasswordAuthentication"
	sshdKeywordKbdInteractiveAuthentication = "KbdInteractiveAuthentication"
	sshdKeywordChallengeResponseAuth        = "ChallengeResponseAuthentication"
	sshdKeywordX11Forwarding                = "X11Forwarding"
	sshdKeywordAllowAgentForwarding         = "AllowAgentForwarding"
	sshdKeywordAllowTCPForwarding           = "AllowTcpForwarding"
	sshdKeywordPermitTunnel                 = "PermitTunnel"
	sshdKeywordPermitOpen                   = "PermitOpen"
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

func sshdSettingValueCommand(keyword, value string) string {
	expected := strings.ToLower(keyword) + " " + value
	configured := keyword + " " + value
	return "out=\"$(sudo sshd -T 2>&1)\"; " +
		"if printf '%s\n' \"$out\" | grep -Fx " + shell.Quote(expected) + "; then exit 0; fi; " +
		"case \"$out\" in *\"Missing privilege separation directory: /run/sshd\"*) " +
		"sudo grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config[.]d/[*][.]conf([[:space:]]|$)' /etc/ssh/sshd_config && " +
		"sudo grep -Fx " + shell.Quote(configured) + " /etc/ssh/sshd_config.d/99-serverpro.conf && " +
		"printf '%s\n' " + shell.Quote("sshd inactive; /run/sshd absent; serverpro config contains "+configured) + " ;; " +
		"*) printf '%s\n' \"$out\"; exit 1 ;; esac"
}

func sshdChallengeResponseCommand() string {
	configured := sshdKeywordChallengeResponseAuth + " " + sshdHardeningExpectations[sshdKeywordChallengeResponseAuth]
	effectiveExpected := strings.ToLower(sshdKeywordKbdInteractiveAuthentication) + " " + sshdHardeningExpectations[sshdKeywordKbdInteractiveAuthentication]
	return "out=\"$(sudo sshd -T 2>&1)\"; " +
		"if printf '%s\n' \"$out\" | grep -Fx " + shell.Quote(effectiveExpected) + "; then exit 0; fi; " +
		"case \"$out\" in *\"Missing privilege separation directory: /run/sshd\"*) " +
		"sudo grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config[.]d/[*][.]conf([[:space:]]|$)' /etc/ssh/sshd_config && " +
		"sudo grep -Fx " + shell.Quote(configured) + " /etc/ssh/sshd_config.d/99-serverpro.conf && " +
		"printf '%s\n' " + shell.Quote("sshd inactive; /run/sshd absent; serverpro config contains "+configured) + " ;; " +
		"*) printf '%s\n' \"$out\"; exit 1 ;; esac"
}

// gitUserCommand runs git as the admin user with the resolved home so the
// root-executed doctor batch reads the user's git configuration, not root's.
func gitUserCommand(user, args string) string {
	quotedUser := shell.Quote(user)
	return "home=\"$(getent passwd " + quotedUser + " | cut -d: -f6)\"; " +
		"runuser -u " + quotedUser + " -- env HOME=\"$home\" git " + args
}

func gitIdentityReadCommand(user string) string {
	return gitUserCommand(user, "config --global user.name") + " >/dev/null && " + gitUserCommand(user, "config --global user.email") + " >/dev/null"
}

func gitIdentityFixCommand(user string, identity config.GitIdentity) string {
	return gitUserCommand(user, "config --global user.name "+shell.Quote(identity.Name)) + " && " +
		gitUserCommand(user, "config --global user.email "+shell.Quote(identity.Email))
}

func gitSigningReadCommand(user string) string {
	return "test \"$(" + gitUserCommand(user, "config --global gpg.format") + ")\" = ssh && " +
		gitUserCommand(user, "config --global user.signingkey") + " >/dev/null"
}

func gitSigningFixCommand(user string) string {
	return gitUserCommand(user, "config --global gpg.format ssh") + " && " +
		gitUserCommand(user, "config --global user.signingkey \"$home/.ssh/id_ed25519_sign.pub\"") + " && " +
		gitUserCommand(user, "config --global commit.gpgsign true")
}

func githubSSHAuthReadCommand(user string) string {
	quotedUser := shell.Quote(user)
	return "home=\"$(getent passwd " + quotedUser + " | cut -d: -f6)\"; " +
		"out=\"$(runuser -u " + quotedUser + " -- env HOME=\"$home\" ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -T git@github.com 2>&1)\"; " +
		"printf '%s\n' \"$out\"; case \"$out\" in *'successfully authenticated'*) exit 0 ;; esac; exit 1"
}

func ghAuthReadCommand(user string) string {
	quotedUser := shell.Quote(user)
	return "home=\"$(getent passwd " + quotedUser + " | cut -d: -f6)\"; " +
		"runuser -u " + quotedUser + " -- env HOME=\"$home\" \"$home/.local/bin/mise\" exec -- gh auth status"
}

func dnsResolutionCommand() string {
	return "getent hosts " + dnsCanaryName + " >/dev/null && echo resolved || { echo 'dns resolution failed for " + dnsCanaryName + "'; exit 1; }"
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

func sudoPasswordFixInput(hash, password string) string {
	return hash + "\n" + password + "\n"
}

func sudoPasswordFixCommand(user string) string {
	return `set -eu
admin_user=` + shell.Quote(user) + `
IFS= read -r hash
IFS= read -r sudo_password
if [ -z "$hash" ]; then
  echo 'admin password hash required' >&2
  exit 1
fi
if [ -z "$sudo_password" ]; then
  echo 'admin sudo password required' >&2
  exit 1
fi
sudoers='/etc/sudoers.d/90-cloud-init-users'
backup_dir='/var/backups/serverpro'
backup="$backup_dir/90-cloud-init-users.before-password-sudo"
tmp="$(mktemp)"
cleanup() { rm -f "$tmp"; }
restore() {
  if [ -f "$backup" ]; then
    install -o root -g root -m 0440 "$backup" "$sudoers"
  fi
}
fail_after_backup() {
  restore
  exit 1
}
trap cleanup EXIT
printf '%s:%s\n' "$admin_user" "$hash" | chpasswd --encrypted
if [ -f "$sudoers" ]; then
  mkdir -p "$backup_dir"
  chmod 0700 "$backup_dir"
  install -o root -g root -m 0400 "$sudoers" "$backup"
  grep -Ev '^[[:space:]]*[^#].*NOPASSWD:ALL' "$sudoers" > "$tmp" || true
  if [ -s "$tmp" ]; then
    visudo -cf "$tmp" || fail_after_backup
    install -o root -g root -m 0440 "$tmp" "$sudoers" || fail_after_backup
  else
    rm -f "$sudoers" || fail_after_backup
  fi
  visudo -c || fail_after_backup
fi
runuser -u "$admin_user" -- sudo -k || true
if ! printf '%s\n' "$sudo_password" | runuser -u "$admin_user" -- sudo -S -p '' -v >/dev/null 2>&1; then
  echo 'admin sudo password validation failed after fix' >&2
  fail_after_backup
fi
runuser -u "$admin_user" -- sudo -k || true
if runuser -u "$admin_user" -- sudo -n true >/dev/null 2>&1; then
  echo 'admin sudo still permits NOPASSWD:ALL after fix' >&2
  fail_after_backup
fi
printf '%s\n' 'admin sudo password requirement fixed'`
}

func sshdSettingsFixCommand() string {
	return `mkdir -p /etc/ssh/sshd_config.d /run/sshd
if ! grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config[.]d/[*][.]conf([[:space:]]|$)' /etc/ssh/sshd_config; then
  tmp="$(mktemp)"
  printf 'Include /etc/ssh/sshd_config.d/*.conf\n' > "$tmp"
  cat /etc/ssh/sshd_config >> "$tmp"
  cat "$tmp" > /etc/ssh/sshd_config
  rm -f "$tmp"
fi
cat > /etc/ssh/sshd_config.d/99-serverpro.conf <<'EOF'
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
sshd -t && systemctl restart ssh`
}
