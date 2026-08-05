package tunnel

import (
	"fmt"

	"github.com/sagmans/serverpro/internal/shell"
)

const cloudflareAPTKeyFingerprint = "CC94B39C77AE7342A68B89628A682D308D4E5E73"

func InstallScript(token string) string {
	return fmt.Sprintf(`set -eu
umask 077

cloudflare_key_tmp=
cloudflare_key_publish_tmp=
cleanup() {
  if [ -n "${cloudflare_key_tmp}" ]; then
    rm -f "${cloudflare_key_tmp}"
  fi
  if [ -n "${cloudflare_key_publish_tmp}" ]; then
    rm -f "${cloudflare_key_publish_tmp}"
  fi
}

install_cloudflare_apt_key() {
  destination=$1
  expected=%s
  cloudflare_key_tmp=$(mktemp)
  curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg -o "${cloudflare_key_tmp}"
  # Trust exactly one approved primary key; subkeys cannot satisfy identity and
  # an appended primary key cannot hide behind the first fingerprint.
  actual=$(gpg --show-keys --with-colons "${cloudflare_key_tmp}" 2>/dev/null | awk -F: '
    $1 == "pub" { primary = 1; next }
    $1 == "sub" { primary = 0; next }
    $1 == "fpr" && primary { print $10; primary = 0 }
  ' | tr -d ' ')
  if [ "${actual}" != "${expected}" ]; then
    printf 'Cloudflare GPG key fingerprint mismatch: expected %%s, got %%s\n' "${expected}" "${actual:-none}" >&2
    rm -f "${cloudflare_key_tmp}"
    cloudflare_key_tmp=
    return 1
  fi
  cloudflare_key_publish_tmp=$(mktemp "${destination}.serverpro.XXXXXX")
  install -m 0644 "${cloudflare_key_tmp}" "${cloudflare_key_publish_tmp}"
  mv -f "${cloudflare_key_publish_tmp}" "${destination}"
  cloudflare_key_publish_tmp=
  rm -f "${cloudflare_key_tmp}"
  cloudflare_key_tmp=
}

main() {
  trap cleanup EXIT
  install -d -m 0755 /usr/share/keyrings /etc/apt/sources.list.d
  install -d -m 0700 /etc/cloudflared
  install_cloudflare_apt_key /usr/share/keyrings/cloudflare-main.gpg
  printf '%%s\n' 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main' > /etc/apt/sources.list.d/cloudflared.list
  chmod 0644 /etc/apt/sources.list.d/cloudflared.list
  apt-get update
  apt-get install -y cloudflared
  id -u cloudflared >/dev/null 2>&1 || useradd --system --home /etc/cloudflared --shell /usr/sbin/nologin cloudflared
  printf '%%s' %s > /etc/cloudflared/token
  chown -R cloudflared:cloudflared /etc/cloudflared
  chmod 0700 /etc/cloudflared
  chmod 0600 /etc/cloudflared/token
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
}

# The guard lets tests execute trust helpers without running privileged setup.
if [ -z "${SERVERPRO_TUNNEL_SOURCE_ONLY:-}" ]; then
  main
fi
`, cloudflareAPTKeyFingerprint, shell.Quote(token))
}
