package tunnel

import (
	"fmt"

	"github.com/assagman/serverpro/internal/shell"
)

// A pinned signer prevents repository setup from trusting a substituted package key.
const (
	cloudflarePackageKeyURL      = "https://pkg.cloudflare.com/cloudflare-main.gpg"
	cloudflareSigningFingerprint = "CC94B39C77AE7342A68B" +
		"89628A682D308D4E5E73"
	cloudflarePackageSource = "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main"
)

const cloudflareInstallScript = `set -eu
umask 077
cloudflare_key="$(mktemp)"
cloudflare_token=
cleanup() {
  rm -f "$cloudflare_key"
  if [ -n "$cloudflare_token" ]; then rm -f "$cloudflare_token"; fi
}
trap cleanup EXIT
install -d -m 0755 /usr/share/keyrings /etc/apt/sources.list.d
curl -fsSL ` + cloudflarePackageKeyURL + ` -o "$cloudflare_key"
key_fingerprint="$(gpg --batch --show-keys --with-colons --fingerprint "$cloudflare_key" | awk -F: '$1 == "pub" { primary = 1; next } primary && $1 == "fpr" { print $10; primary = 0 }')"
if [ "$key_fingerprint" != "` + cloudflareSigningFingerprint + `" ]; then
  printf 'unexpected Cloudflare package key fingerprint: %%s\n' "$key_fingerprint" >&2
  exit 1
fi
install -m 0644 "$cloudflare_key" /usr/share/keyrings/cloudflare-main.gpg
printf '%%s\n' '` + cloudflarePackageSource + `' > /etc/apt/sources.list.d/cloudflared.list
chmod 0644 /etc/apt/sources.list.d/cloudflared.list
apt-get update
apt-get install -y cloudflared
getent group cloudflared >/dev/null 2>&1 || groupadd --system cloudflared
id -u cloudflared >/dev/null 2>&1 || useradd --system --gid cloudflared --home /etc/cloudflared --shell /usr/sbin/nologin cloudflared
# Root owns the directory and atomically replaces the token, so a compromised
# service cannot plant a symlink for a later privileged installer run to follow.
install -d -o root -g cloudflared -m 0750 /etc/cloudflared
cloudflare_token="$(mktemp /etc/cloudflared/.token.XXXXXX)"
printf '%%s' %s > "$cloudflare_token"
chown root:cloudflared "$cloudflare_token"
chmod 0640 "$cloudflare_token"
mv -f "$cloudflare_token" /etc/cloudflared/token
cloudflare_token=""
cat > /etc/systemd/system/cloudflared.service <<'UNIT'
[Unit]
Description=Cloudflare Tunnel connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=cloudflared
Group=cloudflared
ExecStart=/usr/bin/cloudflared --no-autoupdate tunnel run --token-file /etc/cloudflared/token
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now cloudflared
systemctl is-active cloudflared
`

func InstallScript(token string) string {
	return fmt.Sprintf(cloudflareInstallScript, shell.Quote(token))
}
