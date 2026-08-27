package tunnel

import (
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	cloudflareAPTKeyFingerprint = "CC94B39C77AE7342A68B89628A682D308D4E5E73"
	cloudflaredServiceName      = "cloudflared"
)

func CheckCommand() string {
	cloudflared := hostplatform.CloudflaredPackageBaseline()
	return fmt.Sprintf(
		"package_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' %s 2>/dev/null) && case \"${package_record}\" in installed'|'*) installed_version=${package_record#installed|} ;; *) false ;; esac && dpkg --compare-versions \"${installed_version}\" ge %s && systemctl is-active %s",
		shell.Quote(cloudflared.Name),
		shell.Quote(cloudflared.MinimumVersion),
		shell.Quote(cloudflaredServiceName),
	)
}

func InstallScript(token string) string {
	cloudflared := hostplatform.CloudflaredPackageBaseline()
	return fmt.Sprintf(`set -eu
umask 077
export LC_ALL=C

EXPECTED_HOST_OS=%s
EXPECTED_HOST_VERSION=%s
EXPECTED_HOST_CODENAME=%s
EXPECTED_HOST_ARCHITECTURES=%s
CLOUDFLARED_PACKAGE=%s
CLOUDFLARED_MINIMUM_VERSION=%s

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

require_supported_host() {
  . /etc/os-release
  if [ "${ID:-}" != "${EXPECTED_HOST_OS}" ] || [ "${VERSION_ID:-}" != "${EXPECTED_HOST_VERSION}" ] || [ "${VERSION_CODENAME:-}" != "${EXPECTED_HOST_CODENAME}" ]; then
    printf 'unsupported managed host: %%s %%s (%%s)\n' "${ID:-unknown}" "${VERSION_ID:-unknown}" "${VERSION_CODENAME:-unknown}" >&2
    return 1
  fi
  case " ${EXPECTED_HOST_ARCHITECTURES} " in
    *" $(uname -m) "*) ;;
    *)
      printf 'unsupported managed-host architecture: %%s\n' "$(uname -m)" >&2
      return 1
      ;;
  esac
}

installed_cloudflared_version() {
  package_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' "${CLOUDFLARED_PACKAGE}" 2>/dev/null) || return 1
  case "${package_record}" in
    installed'|'*) printf '%%s' "${package_record#installed|}" ;;
    *) return 1 ;;
  esac
}

verify_cloudflared_minimum() {
  installed_version=$(installed_cloudflared_version) || {
    printf 'required package is not installed: %%s\n' "${CLOUDFLARED_PACKAGE}" >&2
    return 1
  }
  if ! dpkg --compare-versions "${installed_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}"; then
    printf 'package %%s is below minimum %%s: %%s\n' "${CLOUDFLARED_PACKAGE}" "${CLOUDFLARED_MINIMUM_VERSION}" "${installed_version}" >&2
    return 1
  fi
}

verify_cloudflared_candidate() {
  if installed_version=$(installed_cloudflared_version) && dpkg --compare-versions "${installed_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}"; then
    return 0
  fi
  candidate_version=$(apt-cache policy "${CLOUDFLARED_PACKAGE}" | awk '$1 == "Candidate:" { print $2; exit }')
  if [ -z "${candidate_version}" ] || [ "${candidate_version}" = '(none)' ]; then
    printf 'cloudflared has no install candidate\n' >&2
    return 1
  fi
  if ! dpkg --compare-versions "${candidate_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}"; then
    printf 'cloudflared candidate is below minimum %%s: %%s\n' "${CLOUDFLARED_MINIMUM_VERSION}" "${candidate_version}" >&2
    return 1
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
  require_supported_host
  trap cleanup EXIT
  install -d -m 0755 /usr/share/keyrings /etc/apt/sources.list.d
  install -d -m 0700 /etc/cloudflared
  install_cloudflare_apt_key /usr/share/keyrings/cloudflare-main.gpg
  printf '%%s\n' 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared %s main' > /etc/apt/sources.list.d/cloudflared.list
  chmod 0644 /etc/apt/sources.list.d/cloudflared.list
  apt-get update
  verify_cloudflared_candidate
  apt-get install -y "${CLOUDFLARED_PACKAGE}"
  verify_cloudflared_minimum
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
`,
		hostplatform.ManagedHostOS,
		hostplatform.ManagedHostVersion,
		hostplatform.ManagedHostCodename,
		shell.Quote(strings.Join(hostplatform.ManagedHostKernelArchitectures(), " ")),
		cloudflared.Name,
		cloudflared.MinimumVersion,
		cloudflareAPTKeyFingerprint,
		hostplatform.ManagedHostCodename,
		shell.Quote(token),
	)
}
