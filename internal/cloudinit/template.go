package cloudinit

// A pinned signer keeps first boot fail-closed if the package-key download path is compromised.
const (
	tailscalePackageKeyURL      = "https://pkgs.tailscale.com/stable/ubuntu/noble.noarmor.gpg"
	tailscaleSigningFingerprint = "2596A99EAAB33821893C0A79458CA832957F5868"
	tailscalePackageSource      = "deb [signed-by=/usr/share/keyrings/tailscale-archive-keyring.gpg] https://pkgs.tailscale.com/stable/ubuntu noble main"
)

const cloudInitTemplate = `#cloud-config
package_update: true
package_upgrade: true
packages:
  - curl
  - ca-certificates
  - gnupg
  - ufw
  - apparmor
  - unattended-upgrades
  - jq
users:
  - default
  - name: {{ .Config.Admin.Username }}
    groups: sudo
    shell: /bin/bash
    hashed_passwd: {{ .AdminPasswordHash }}
    lock_passwd: false
ssh_pwauth: false
disable_root: true
write_files:
  - path: /etc/ssh/sshd_config.d/99-serverpro.conf
    permissions: '0644'
    content: |
      PermitRootLogin no
      PasswordAuthentication no
      KbdInteractiveAuthentication no
      ChallengeResponseAuthentication no
      X11Forwarding no
      AllowAgentForwarding no
      AllowTcpForwarding no
      PermitTunnel no
      PermitOpen none
  - path: /etc/systemd/journald.conf.d/99-serverpro.conf
    permissions: '0644'
    content: |
      [Journal]
      Storage=persistent
  - path: /etc/sysctl.d/99-serverpro.conf
    permissions: '0644'
    content: |
      net.ipv4.conf.all.rp_filter=1
      net.ipv4.conf.default.rp_filter=1
      net.ipv4.tcp_syncookies=1
      net.ipv6.conf.all.accept_ra=0
  - path: /root/.serverpro-tailscale-authkey
    permissions: '0600'
    content: |
      {{ .TailscaleAuthKey }}
  - path: /var/lib/serverpro/bootstrap.json
    permissions: '0644'
    content: |
      {"managed_by":"serverpro","namespace":{{ jsonString .Config.Project }}}
runcmd:
  - mkdir -p /var/log/journal /var/lib/serverpro
  - systemctl restart systemd-journald
  - systemctl enable --now apparmor
  - systemctl enable --now unattended-upgrades
  - ufw --force reset
  - ufw default deny incoming
  - ufw default allow outgoing
  - ufw --force enable
  - |
    if grep -Eq '^[[:space:]]*#?[[:space:]]*PermitRootLogin[[:space:]]+' /etc/ssh/sshd_config; then
      sed -i -E 's/^[[:space:]]*#?[[:space:]]*PermitRootLogin[[:space:]]+.*/PermitRootLogin no/' /etc/ssh/sshd_config
    else
      tmp="$(mktemp)"
      printf 'PermitRootLogin no\n' > "$tmp"
      cat /etc/ssh/sshd_config >> "$tmp"
      cat "$tmp" > /etc/ssh/sshd_config
      rm -f "$tmp"
    fi
  - systemctl restart ssh
  - |
    set -eu
    umask 077
    tailscale_key="$(mktemp)"
    cleanup() {
      rm -f "$tailscale_key" /root/.serverpro-tailscale-authkey /var/lib/cloud/instances/*/user-data.txt /var/lib/cloud/instances/*/user-data.txt.i
    }
    trap cleanup EXIT
    curl -fsSL ` + tailscalePackageKeyURL + ` -o "$tailscale_key"
    key_fingerprint="$(gpg --batch --show-keys --with-colons --fingerprint "$tailscale_key" | awk -F: '$1 == "pub" { primary = 1; next } primary && $1 == "fpr" { print $10; primary = 0 }')"
    if [ "$key_fingerprint" != "` + tailscaleSigningFingerprint + `" ]; then
      printf 'unexpected Tailscale package key fingerprint: %s\n' "$key_fingerprint" >&2
      exit 1
    fi
    install -d -m 0755 /usr/share/keyrings /etc/apt/sources.list.d
    install -m 0644 "$tailscale_key" /usr/share/keyrings/tailscale-archive-keyring.gpg
    printf '%s\n' '` + tailscalePackageSource + `' > /etc/apt/sources.list.d/tailscale.list
    chmod 0644 /etc/apt/sources.list.d/tailscale.list
    apt-get update
    apt-get install -y tailscale
    systemctl enable --now tailscaled
    tailscale up --auth-key "$(cat /root/.serverpro-tailscale-authkey)" --ssh --hostname {{ shellQuote .Config.Compute.Name }} --advertise-tags {{ shellQuote (join .Config.Access.Tailscale.Tags ",") }}
final_message: "serverpro bootstrap complete"
`
