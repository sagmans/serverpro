#!/usr/bin/env bash
# shellcheck shell=bash
if [ -z "${BASH_VERSION:-}" ] || [ -n "${POSIXLY_CORRECT:-}" ]; then
  unset POSIXLY_CORRECT
  exec bash "$0" "$@"
fi
set -euo pipefail
umask 022

TAILSCALE_TMP_DIR=
TAILSCALE_PUBLISH_TEMPS=()
TAILSCALE_DEFAULTS_PATH=/etc/default/tailscaled

cleanup() {
  if [[ -n ${TAILSCALE_TMP_DIR} ]]; then
    rm -rf "${TAILSCALE_TMP_DIR}" 2>/dev/null || true
  fi
  if [[ ${#TAILSCALE_PUBLISH_TEMPS[@]} -gt 0 ]]; then
    rm -f "${TAILSCALE_PUBLISH_TEMPS[@]}" 2>/dev/null || true
  fi
}

require_root() {
  if [[ ${EUID} -ne 0 ]]; then
    printf 'Tailscale update must run as root.\n' >&2
    exit 1
  fi
}

required_env() {
  local name=$1 value=${!1:-}
  if [[ -z ${value} ]]; then
    printf '%s is required for Tailscale update.\n' "${name}" >&2
    exit 1
  fi
  printf '%s' "${value}"
}

install_prerequisites() {
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl jq
}

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
    amd64) required_env SERVERPRO_TAILSCALE_SHA256_AMD64 ;;
    arm64) required_env SERVERPRO_TAILSCALE_SHA256_ARM64 ;;
    *) return 1 ;;
  esac
}

publish_file() {
  local source=$1 destination=$2 mode=$3 temporary
  temporary=${destination}.serverpro.$$
  TAILSCALE_PUBLISH_TEMPS+=("${temporary}")
  install -o root -g root -m "${mode}" "${source}" "${temporary}"
  mv -f "${temporary}" "${destination}"
}

ensure_tlsmlkem_default() {
  install -d -m 0755 "$(dirname "${TAILSCALE_DEFAULTS_PATH}")"
  if [[ ! -e ${TAILSCALE_DEFAULTS_PATH} ]]; then
    install -o root -g root -m 0644 /dev/null "${TAILSCALE_DEFAULTS_PATH}"
  fi
  if ! grep -Fxq 'GODEBUG="tlsmlkem=1"' "${TAILSCALE_DEFAULTS_PATH}"; then
    printf '\nGODEBUG="tlsmlkem=1"\n' >>"${TAILSCALE_DEFAULTS_PATH}"
  fi
}

install_tailscale_update() {
  local version arch checksum archive root delay unit
  version=$(required_env SERVERPRO_TAILSCALE_VERSION)
  arch=$(tailscale_arch)
  checksum=$(tailscale_sha256 "${arch}")
  delay=$(required_env SERVERPRO_TAILSCALE_RESTART_DELAY)
  archive=tailscale_${version}_${arch}.tgz
  root=tailscale_${version}_${arch}
  TAILSCALE_TMP_DIR=$(mktemp -d)
  chmod 0700 "${TAILSCALE_TMP_DIR}"

  curl -fsSL "https://pkgs.tailscale.com/stable/${archive}" -o "${TAILSCALE_TMP_DIR}/${archive}"
  printf '%s  %s\n' "${checksum}" "${archive}" >"${TAILSCALE_TMP_DIR}/checksums"
  (cd "${TAILSCALE_TMP_DIR}" && sha256sum -c checksums)
  tar --extract --gzip --file "${TAILSCALE_TMP_DIR}/${archive}" --directory "${TAILSCALE_TMP_DIR}" --no-same-owner --no-same-permissions \
    "${root}/tailscale" \
    "${root}/tailscaled" \
    "${root}/systemd/tailscaled.service" \
    "${root}/systemd/tailscaled.defaults" \
    "${root}/systemd/tailscale-online.target" \
    "${root}/systemd/tailscale-wait-online.service"

  install -d -m 0755 /etc/default /etc/systemd/system /usr/bin /usr/sbin
  publish_file "${TAILSCALE_TMP_DIR}/${root}/tailscale" /usr/bin/tailscale 0755
  publish_file "${TAILSCALE_TMP_DIR}/${root}/tailscaled" /usr/sbin/tailscaled 0755
  publish_file "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscaled.service" /etc/systemd/system/tailscaled.service 0644
  publish_file "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscale-online.target" /etc/systemd/system/tailscale-online.target 0644
  publish_file "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscale-wait-online.service" /etc/systemd/system/tailscale-wait-online.service 0644
  if [[ ! -e ${TAILSCALE_DEFAULTS_PATH} ]]; then
    publish_file "${TAILSCALE_TMP_DIR}/${root}/systemd/tailscaled.defaults" "${TAILSCALE_DEFAULTS_PATH}" 0644
  fi
  ensure_tlsmlkem_default

  if [[ $(/usr/bin/tailscale version --json | jq -r '.short // empty') != "${version}" ]]; then
    printf 'installed Tailscale client does not report %s.\n' "${version}" >&2
    exit 1
  fi
  systemctl daemon-reload
  systemctl enable tailscaled >/dev/null
  unit=serverpro-tailscaled-restart-$$
  # Restart after this Tailscale SSH command returns; an inline restart would
  # terminate the transport before serverpro could distinguish success.
  systemd-run --quiet --unit="${unit}" --on-active="${delay}" --property=Type=oneshot /bin/systemctl restart tailscaled
  printf 'staged Tailscale %s; delayed daemon restart scheduled\n' "${version}"
}

main() {
  trap cleanup EXIT
  require_root
  # Reject unsupported hosts before apt changes machine state.
  tailscale_arch >/dev/null
  install_prerequisites
  install_tailscale_update
}

if [[ -z ${SERVERPRO_TAILSCALE_SOURCE_ONLY:-} ]]; then
  main "$@"
fi
