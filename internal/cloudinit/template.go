package cloudinit

import (
	"strings"

	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/tailscaletools"
)

const (
	tailscaleVersion     = tailscaletools.Version
	tailscaleAMD64SHA256 = tailscaletools.AMD64SHA256
	tailscaleARM64SHA256 = tailscaletools.ARM64SHA256
)

var cloudInitTemplate = `#cloud-config
bootcmd:
  - |
    set -eu
    EXPECTED_HOST_OS=` + hostplatform.ManagedHostOS + `
    EXPECTED_HOST_VERSION=` + hostplatform.ManagedHostVersion + `
    EXPECTED_HOST_CODENAME=` + hostplatform.ManagedHostCodename + `
    EXPECTED_HOST_ARCHITECTURES='` + strings.Join(hostplatform.ManagedHostKernelArchitectures(), " ") + `'
    . /etc/os-release
    if [ "${ID:-}" != "${EXPECTED_HOST_OS}" ] || [ "${VERSION_ID:-}" != "${EXPECTED_HOST_VERSION}" ] || [ "${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}" != "${EXPECTED_HOST_CODENAME}" ]; then
      printf 'unsupported managed host: %s %s (%s)\n' "${ID:-unknown}" "${VERSION_ID:-unknown}" "${VERSION_CODENAME:-${UBUNTU_CODENAME:-unknown}}" >&2
      exit 1
    fi
    case " ${EXPECTED_HOST_ARCHITECTURES} " in
      *" $(uname -m) "*) ;;
      *)
        printf 'unsupported managed-host architecture: %s\n' "$(uname -m)" >&2
        exit 1
        ;;
    esac
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
      {"managed_by":"serverpro","namespace":{{ jsonString .Config.Namespace }}}
runcmd:
  - |
    set -eu
    export LC_ALL=C
    TAILSCALE_TMP_DIR=
    TAILSCALE_AUTH_KEY=$(cat /root/.serverpro-tailscale-authkey)

    cleanup() {
      unset TAILSCALE_AUTH_KEY
      rm -f /root/.serverpro-tailscale-authkey /var/lib/cloud/instances/*/user-data.txt /var/lib/cloud/instances/*/user-data.txt.i
      if [ -n "${TAILSCALE_TMP_DIR}" ]; then
        rm -rf "${TAILSCALE_TMP_DIR}"
      fi
    }

    trap cleanup EXIT
    trap 'exit 1' HUP INT TERM
    rm -f /root/.serverpro-tailscale-authkey /var/lib/cloud/instances/*/user-data.txt /var/lib/cloud/instances/*/user-data.txt.i

    EXPECTED_HOST_OS=` + hostplatform.ManagedHostOS + `
    EXPECTED_HOST_VERSION=` + hostplatform.ManagedHostVersion + `
    EXPECTED_HOST_CODENAME=` + hostplatform.ManagedHostCodename + `
    EXPECTED_HOST_ARCHITECTURES='` + strings.Join(hostplatform.ManagedHostKernelArchitectures(), " ") + `'
    . /etc/os-release
    if [ "${ID:-}" != "${EXPECTED_HOST_OS}" ] || [ "${VERSION_ID:-}" != "${EXPECTED_HOST_VERSION}" ] || [ "${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}" != "${EXPECTED_HOST_CODENAME}" ]; then
      printf 'unsupported managed host: %s %s (%s)\n' "${ID:-unknown}" "${VERSION_ID:-unknown}" "${VERSION_CODENAME:-${UBUNTU_CODENAME:-unknown}}" >&2
      exit 1
    fi
    case " ${EXPECTED_HOST_ARCHITECTURES} " in
      *" $(uname -m) "*) ;;
      *)
        printf 'unsupported managed-host architecture: %s\n' "$(uname -m)" >&2
        exit 1
        ;;
    esac

    PACKAGE_BASELINES='` + strings.ReplaceAll(hostplatform.PackageBaselineManifest(hostplatform.BasePackageBaselines()), "\n", "\n    ") + `'

    installed_package_version() {
      package_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' "$1" 2>/dev/null) || return 1
      case "${package_record}" in
        installed'|'*) printf '%s' "${package_record#installed|}" ;;
        *) return 1 ;;
      esac
    }

    verify_package_candidates() {
      printf '%s\n' "${PACKAGE_BASELINES}" | while IFS='|' read -r package_name minimum_version; do
        if installed_version=$(installed_package_version "${package_name}") && dpkg --compare-versions "${installed_version}" ge "${minimum_version}"; then
          continue
        fi
        candidate_version=$(apt-cache policy "${package_name}" | awk '$1 == "Candidate:" { print $2; exit }')
        if [ -z "${candidate_version}" ] || [ "${candidate_version}" = '(none)' ]; then
          printf 'required package has no install candidate: %s\n' "${package_name}" >&2
          return 1
        fi
        if ! dpkg --compare-versions "${candidate_version}" ge "${minimum_version}"; then
          printf 'package candidate %s is below minimum %s: %s\n' "${package_name}" "${minimum_version}" "${candidate_version}" >&2
          return 1
        fi
      done
    }

    verify_package_minimums() {
      printf '%s\n' "${PACKAGE_BASELINES}" | while IFS='|' read -r package_name minimum_version; do
        installed_version=$(installed_package_version "${package_name}") || {
          printf 'required package is not installed: %s\n' "${package_name}" >&2
          return 1
        }
        if ! dpkg --compare-versions "${installed_version}" ge "${minimum_version}"; then
          printf 'package %s is below minimum %s: %s\n' "${package_name}" "${minimum_version}" "${installed_version}" >&2
          return 1
        fi
      done
    }

    apt-get update
    verify_package_candidates
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ` + strings.Join(hostplatform.PackageNames(hostplatform.BasePackageBaselines()), " ") + `
    verify_package_minimums
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
    TAILSCALE_VERSION=` + tailscaleVersion + `
    TAILSCALE_SHA256_AMD64=` + tailscaleAMD64SHA256 + `
    TAILSCALE_SHA256_ARM64=` + tailscaleARM64SHA256 + `
    TAILSCALE_TMP_DIR=${TAILSCALE_TMP_DIR:-}
    TAILSCALE_DEFAULTS_PATH=/etc/default/tailscaled
    if [ -n "${SERVERPRO_TAILSCALE_SOURCE_ONLY:-}" ]; then
      # Executable tests redirect this append away from the host; production
      # cloud-init never accepts an alternate trusted service path.
      TAILSCALE_DEFAULTS_PATH=${SERVERPRO_TAILSCALE_DEFAULTS_PATH:-${TAILSCALE_DEFAULTS_PATH}}
    fi

    tailscale_arch() {
      case "$(uname -m)" in
        x86_64) printf 'amd64' ;;
        aarch64|arm64) printf 'arm64' ;;
        *)
          printf 'unsupported Tailscale architecture: %s\n' "$(uname -m)" >&2
          return 1
          ;;
      esac
    }

    tailscale_sha256() {
      case "$1" in
        amd64) printf '%s' "${TAILSCALE_SHA256_AMD64}" ;;
        arm64) printf '%s' "${TAILSCALE_SHA256_ARM64}" ;;
        *) return 1 ;;
      esac
    }

    install_tailscale() {
      arch=$(tailscale_arch)
      checksum=$(tailscale_sha256 "${arch}")
      archive="tailscale_${TAILSCALE_VERSION}_${arch}.tgz"
      root="tailscale_${TAILSCALE_VERSION}_${arch}"
      TAILSCALE_TMP_DIR=$(mktemp -d)
      chmod 0700 "${TAILSCALE_TMP_DIR}"

      curl -fsSL "https://pkgs.tailscale.com/stable/${archive}" -o "${TAILSCALE_TMP_DIR}/${archive}"
      printf '%s  %s\n' "${checksum}" "${archive}" > "${TAILSCALE_TMP_DIR}/checksums"
      (cd "${TAILSCALE_TMP_DIR}" && sha256sum -c checksums)
      tar --extract --gzip --file "${TAILSCALE_TMP_DIR}/${archive}" --directory "${TAILSCALE_TMP_DIR}" --no-same-owner --no-same-permissions \
        "${root}/tailscale" \
        "${root}/tailscaled" \
        "${root}/systemd/tailscaled.service" \
        "${root}/systemd/tailscaled.defaults" \
        "${root}/systemd/tailscale-online.target" \
        "${root}/systemd/tailscale-wait-online.service"

      install -d -m 0755 /etc/default /etc/systemd/system /usr/bin /usr/sbin
      install -o root -g root -m 0755 "${TAILSCALE_TMP_DIR}/${root}/tailscale" /usr/bin/tailscale
      install -o root -g root -m 0755 "${TAILSCALE_TMP_DIR}/${root}/tailscaled" /usr/sbin/tailscaled
      install -o root -g root -m 0644 "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscaled.service" /etc/systemd/system/tailscaled.service
      install -o root -g root -m 0644 "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscaled.defaults" /etc/default/tailscaled
      install -o root -g root -m 0644 "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscale-online.target" /etc/systemd/system/tailscale-online.target
      install -o root -g root -m 0644 "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscale-wait-online.service" /etc/systemd/system/tailscale-wait-online.service
      # Preserve the reviewed hybrid TLS default independently from the
      # upstream artifact's embedded build setting.
      printf '\nGODEBUG="tlsmlkem=1"\n' >> "${TAILSCALE_DEFAULTS_PATH}"
      systemctl daemon-reload
      systemctl enable --now tailscaled
    }

    main() {
      install_tailscale
      tailscale up --auth-key "${TAILSCALE_AUTH_KEY}" --ssh --hostname {{ shellQuote .Config.Compute.Name }} --advertise-tags {{ shellQuote (join .Config.Access.Tailscale.Tags ",") }}
    }

    # The guard lets tests execute architecture checks without host mutation.
    if [ -z "${SERVERPRO_TAILSCALE_SOURCE_ONLY:-}" ]; then
      main
    fi
final_message: "serverpro bootstrap complete"
`
