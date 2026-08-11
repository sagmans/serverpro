#!/usr/bin/env bash
# shellcheck shell=bash
if [ -z "${BASH_VERSION:-}" ] || [ -n "${POSIXLY_CORRECT:-}" ]; then
  exec bash "$0" "$@"
fi
set -euo pipefail
# Deterministic modes for privileged writes; explicit `install -m` still overrides.
umask 022

# Pinned artifacts are fetched/extracted under root; remove every temp dir on any
# exit path (error, signal, success) so partial downloads never linger on a host.
BOOTSTRAP_TMP_DIRS=()
register_tmp() {
  BOOTSTRAP_TMP_DIRS+=("$1")
}
cleanup_tmp_dirs() {
  if [[ ${#BOOTSTRAP_TMP_DIRS[@]} -gt 0 ]]; then
    # Best-effort: never let cleanup mask a real failure or abort the script.
    rm -rf "${BOOTSTRAP_TMP_DIRS[@]}" 2>/dev/null || true
  fi
}
trap cleanup_tmp_dirs EXIT

log() {
  printf '[serverpro-bootstrap-tools] %s\n' "$*" >&2
}

warn() {
  printf '[serverpro-bootstrap-tools] warning: %s\n' "$*" >&2
}

require_root() {
  if [[ ${EUID} -ne 0 ]]; then
    printf 'serverpro-bootstrap-tools must run as root, usually via sudo.\n' >&2
    exit 1
  fi
}

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

required_env() {
  local name="$1"
  local value="${!name:-}"
  if [[ -z ${value} ]]; then
    printf '%s is required for serverpro bootstrap. Use scripts/serverpro-bootstrap-tools.sh or serverpro server bootstrap.\n' "${name}" >&2
    exit 1
  fi
  printf '%s' "${value}"
}

validate_version_token() {
  local name="$1"
  local value="$2"
  case "${value}" in
    ''|*[!A-Za-z0-9._+-]*)
      printf '%s contains unsupported characters: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
}

validate_sha256_token() {
  local name="$1"
  local value="$2"
  case "${value}" in
    *[!A-Fa-f0-9]*)
      printf '%s must be a SHA-256 hex digest.\n' "${name}" >&2
      exit 1
      ;;
  esac
  if [[ ${#value} -ne 64 ]]; then
    printf '%s must be a SHA-256 hex digest.\n' "${name}" >&2
    exit 1
  fi
}

validate_tool_name() {
  local name="$1"
  local value="$2"
  case "${value}" in
    ''|*[!A-Za-z0-9@._/+-]*)
      printf '%s contains unsupported characters: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
}

validate_mise_backend() {
  local name="$1"
  local value="$2"
  local repository
  case "${value}" in
    github:*) repository=${value#github:} ;;
    *)
      printf '%s must use a github:owner/repository backend: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
  case "${repository}" in
    ''|*[!A-Za-z0-9._/-]*|/*|*/|*//*|*/*/*)
      printf '%s must use a github:owner/repository backend: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
    */*) ;;
    *)
      printf '%s must use a github:owner/repository backend: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
}

validate_mise_tool_backend() {
  local name="$1"
  local value="$2"
  local source
  case "${value}" in
    aqua:*|core:*|github:*) source=${value#*:} ;;
    *)
      printf '%s must use an aqua:, core:, or github: backend: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
  case "${source}" in
    ''|*[!A-Za-z0-9._/-]*|/*|*/|*//*)
      printf '%s contains an invalid backend source: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
}

validate_env_name() {
  local name="$1" value="$2"
  case "${value}" in
    ''|*[!A-Z0-9_]*)
      printf '%s contains an invalid environment name: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
}

validate_config_key() {
  local name="$1" value="$2"
  case "${value}" in
    ''|*[!A-Za-z0-9._-]*)
      printf '%s contains an invalid mise config key: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
}

validate_user_token() {
  local name="$1"
  local value="$2"
  case "${value}" in
    ''|*[!A-Za-z0-9._-]*)
      printf '%s contains unsupported characters: %s\n' "${name}" "${value}" >&2
      exit 1
      ;;
  esac
}

validate_package_token() {
  local package="$1"
  local package_name
  case "${package}" in
    apt:*) ;;
    *)
      printf 'unsupported package token: %s\n' "${package}" >&2
      exit 1
      ;;
  esac
  package_name=${package#apt:}
  case "${package_name}" in
    ''|*[!A-Za-z0-9.+-]*)
      printf 'unsupported package token: %s\n' "${package}" >&2
      exit 1
      ;;
  esac
}

bootstrap_version_env() {
  local name="$1"
  local value
  value=$(required_env "${name}")
  validate_version_token "${name}" "${value}"
  printf '%s' "${value}"
}

bootstrap_min_mise_version() {
  bootstrap_version_env SERVERPRO_BOOTSTRAP_MIN_MISE_VERSION
}

# bootstrap_sha256_env reads and validates one pinned SHA-256 manifest entry.
# Callers pass the variable name directly so no per-artifact wrapper functions
# (and their drift) are needed.
bootstrap_sha256_env() {
  local name="$1"
  local value
  value=$(required_env "${name}")
  validate_sha256_token "${name}" "${value}"
  printf '%s' "${value}"
}

managed_mise_tool_rows() {
  required_env SERVERPRO_BOOTSTRAP_MISE_TOOLS
}

managed_mise_tool_row() {
  local wanted=$1 row key
  while IFS= read -r row; do
    IFS='|' read -r key _ <<<"${row}"
    if [[ ${key} == "${wanted}" ]]; then
      printf '%s' "${row}"
      return 0
    fi
  done <<<"$(managed_mise_tool_rows)"
  printf 'managed mise tool missing from manifest: %s\n' "${wanted}" >&2
  return 1
}

managed_mise_tool_version() {
  local row version_env
  row=$(managed_mise_tool_row "$1")
  IFS='|' read -r _ version_env _ <<<"${row}"
  bootstrap_version_env "${version_env}"
}

managed_mise_tool_sha256_for_arch() {
  local row=$1 arch=$2 key checksum_key checksum_x64_env checksum_arm64_env env_name
  IFS='|' read -r key _ _ _ _ _ _ checksum_key checksum_x64_env checksum_arm64_env _ <<<"${row}"
  if [[ ${checksum_key} == - || ${checksum_x64_env} == - || ${checksum_arm64_env} == - ]]; then
    printf 'managed mise tool has no release checksum: %s\n' "${key}" >&2
    return 1
  fi
  case "${arch}" in
    x86_64) env_name=${checksum_x64_env} ;;
    aarch64|arm64) env_name=${checksum_arm64_env} ;;
    *)
      printf 'unsupported architecture for %s release: %s\n' "${key}" "${arch}" >&2
      return 1
      ;;
  esac
  bootstrap_sha256_env "${env_name}"
}

bootstrap_node_version() {
  managed_mise_tool_version node
}

bootstrap_pi_version() {
  bootstrap_version_env SERVERPRO_BOOTSTRAP_PI_VERSION
}

bootstrap_herdr_version() {
  bootstrap_version_env SERVERPRO_BOOTSTRAP_HERDR_VERSION
}

bootstrap_herdr_backend() {
  local backend
  backend=$(required_env SERVERPRO_BOOTSTRAP_HERDR_BACKEND)
  validate_mise_backend SERVERPRO_BOOTSTRAP_HERDR_BACKEND "${backend}"
  printf '%s' "${backend}"
}

bootstrap_herdr_sha256_for_arch() {
  # Herdr ships Linux x86_64/arm64 binaries only; map the arch to its manifest
  # entry and fail closed on anything else.
  local env_name
  case "$1" in
    x86_64) env_name=SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64 ;;
    aarch64|arm64) env_name=SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64 ;;
    *)
      printf 'unsupported architecture for Herdr release: %s\n' "$1" >&2
      exit 1
      ;;
  esac
  bootstrap_sha256_env "${env_name}"
}

bootstrap_pi_tool() {
  local tool
  tool=$(required_env SERVERPRO_BOOTSTRAP_PI_TOOL)
  validate_tool_name SERVERPRO_BOOTSTRAP_PI_TOOL "${tool}"
  printf '%s' "${tool}"
}

read_package_env() {
  local name="$1"
  # A plain nameref name such as `out` triggers a bash "circular name reference"
  # when a caller passes a variable of the same name, silently yielding an unbound
  # array that aborts under `set -u`. Use a caller-unlikely prefixed name instead.
  local -n _rpe_out="$2"
  local value
  value=$(required_env "${name}")
  read -r -a _rpe_out <<<"${value}"
  if [[ ${#_rpe_out[@]} -eq 0 ]]; then
    printf '%s must contain at least one package.\n' "${name}" >&2
    exit 1
  fi
  local package
  for package in "${_rpe_out[@]}"; do
    validate_package_token "${package}"
  done
}

APT_UPDATED=0
MISE_PACKAGE_UPDATED=0
DOCKER_CONFIG_CHANGED=0
ROOT_MISE=/usr/local/bin/mise
# Docker release-signing key fingerprint (pubkey 0EBFCD88), published at
# https://download.docker.com/linux/ubuntu/gpg. Pinned so a compromised package
# endpoint cannot swap in a key that signs malicious apt packages.
DOCKER_GPG_FINGERPRINT=9DC858229FC7DD38854AE2D88D81803C0EBFCD88
BOOTSTRAP_TARGET=
TARGET_USER=
TARGET_HOME=
TARGET_GID=
TARGET_PATH=

apt_update_once() {
  if [[ ${APT_UPDATED} -eq 0 ]]; then
    log 'updating apt package index'
    apt-get update
    APT_UPDATED=1
  fi
}

apt_install() {
  apt_update_once
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@"
}

atomic_install_file() {
  local src=$1
  local dst=$2
  local mode=$3
  local owner=${4:-root}
  local group=${5:-root}

  if [[ -f ${dst} ]] && cmp -s "${src}" "${dst}"; then
    rm -f "${src}"
    return 1
  fi

  install -m "${mode}" -o "${owner}" -g "${group}" "${src}" "${dst}"
  rm -f "${src}"
  return 0
}

# verify_gpg_fingerprint requires the imported keyring to contain exactly one
# approved primary key. Subkey fingerprints remain valid but cannot satisfy the
# trust decision, and an appended primary key cannot hide behind the first match.
verify_gpg_fingerprint() {
  local keyfile="$1"
  local expected="$2"
  local label="$3"
  local actual
  actual=$(gpg --show-keys --with-fingerprint --with-colons "${keyfile}" 2>/dev/null \
    | awk -F: '
        $1 == "pub" { primary = 1; next }
        $1 == "sub" { primary = 0; next }
        $1 == "fpr" && primary { print $10; primary = 0 }
      ' \
    | tr -d ' ')
  if [[ ${actual} != "${expected}" ]]; then
    printf '%s GPG key fingerprint mismatch: expected %s, got %s\n' "${label}" "${expected}" "${actual:-none}" >&2
    exit 1
  fi
}

require_supported_os() {
  if [[ ! -r /etc/os-release ]]; then
    printf 'missing /etc/os-release; unsupported OS.\n' >&2
    exit 1
  fi

  # shellcheck source=/dev/null
  . /etc/os-release
  case "${ID:-}" in
    ubuntu|debian) ;;
    *)
      printf 'unsupported OS: %s. Expected Ubuntu or Debian.\n' "${ID:-unknown}" >&2
      exit 1
      ;;
  esac
}

validate_bootstrap_target() {
  case "${BOOTSTRAP_TARGET}" in
    all|git|docker|mise|node|pi) ;;
    *)
      printf 'unsupported bootstrap target: %s\n' "${BOOTSTRAP_TARGET}" >&2
      exit 1
      ;;
  esac
}

validate_managed_mise_manifest() {
  local row key version_env alias_key backend_env version_key profile_key profile_env checksum_key checksum_x64_env checksum_arm64_env force probe extra
  local backend profile required
  local seen=' ' count=0
  while IFS= read -r row; do
    backend=
    IFS='|' read -r key version_env alias_key backend_env version_key profile_key profile_env checksum_key checksum_x64_env checksum_arm64_env force probe extra <<<"${row}"
    if [[ -n ${extra:-} ]]; then
      printf 'managed mise manifest row has too many fields: %s\n' "${row}" >&2
      exit 1
    fi
    validate_user_token managed_mise_tool "${key}"
    if [[ ${probe} != "${key}" ]]; then
      printf 'managed mise probe mismatch for %s: %s\n' "${key}" "${probe}" >&2
      exit 1
    fi
    if [[ ${seen} == *" ${key} "* ]]; then
      printf 'duplicate managed mise tool: %s\n' "${key}" >&2
      exit 1
    fi
    seen+="${key} "
    ((count += 1))

    validate_env_name managed_mise_version_env "${version_env}"
    bootstrap_version_env "${version_env}" >/dev/null
    validate_config_key managed_mise_version_key "${version_key}"

    if [[ ${alias_key} == - || ${backend_env} == - ]]; then
      if [[ ${alias_key} != - || ${backend_env} != - ]]; then
        printf 'managed mise alias fields must be paired for %s.\n' "${key}" >&2
        exit 1
      fi
    else
      validate_config_key managed_mise_alias_key "${alias_key}"
      validate_env_name managed_mise_backend_env "${backend_env}"
      backend=$(required_env "${backend_env}")
      validate_mise_tool_backend "${backend_env}" "${backend}"
    fi

    if [[ ${profile_key} == - || ${profile_env} == - ]]; then
      if [[ ${profile_key} != - || ${profile_env} != - ]]; then
        printf 'managed mise profile fields must be paired for %s.\n' "${key}" >&2
        exit 1
      fi
    else
      validate_config_key managed_mise_profile_key "${profile_key}"
      validate_env_name managed_mise_profile_env "${profile_env}"
      profile=$(required_env "${profile_env}")
      validate_version_token "${profile_env}" "${profile}"
      if [[ ${key} == rust && ${profile} != default ]]; then
        printf '%s must be default: %s\n' "${profile_env}" "${profile}" >&2
        exit 1
      fi
    fi

    if [[ ${checksum_key} == - || ${checksum_x64_env} == - || ${checksum_arm64_env} == - ]]; then
      if [[ ${checksum_key} != - || ${checksum_x64_env} != - || ${checksum_arm64_env} != - ]]; then
        printf 'managed mise checksum fields must be complete for %s.\n' "${key}" >&2
        exit 1
      fi
    else
      validate_config_key managed_mise_checksum_key "${checksum_key}"
      validate_env_name managed_mise_checksum_x64_env "${checksum_x64_env}"
      validate_env_name managed_mise_checksum_arm64_env "${checksum_arm64_env}"
      bootstrap_sha256_env "${checksum_x64_env}" >/dev/null
      bootstrap_sha256_env "${checksum_arm64_env}" >/dev/null
      if [[ ${backend} != github:* ]]; then
        printf 'managed mise checksums require a github backend for %s.\n' "${key}" >&2
        exit 1
      fi
    fi

    case "${force}" in true|false) ;; *) printf 'invalid force policy for %s: %s\n' "${key}" "${force}" >&2; exit 1 ;; esac
    if [[ ${checksum_key} != - && ${force} != true ]]; then
      printf 'checksum-managed mise tool must force repair: %s\n' "${key}" >&2
      exit 1
    fi
  done <<<"$(managed_mise_tool_rows)"
  for required in node uv rust tmux gh rg fd ast-grep sem inspect; do
    if [[ ${seen} != *" ${required} "* ]]; then
      printf 'managed mise manifest missing required tool: %s\n' "${required}" >&2
      exit 1
    fi
  done
  if [[ ${count} -ne 10 ]]; then
    printf 'managed mise manifest must contain exactly 10 tools, got %s.\n' "${count}" >&2
    exit 1
  fi
}

validate_bootstrap_env() {
  bootstrap_min_mise_version >/dev/null
  bootstrap_sha256_env SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_X64 >/dev/null
  bootstrap_sha256_env SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARM64 >/dev/null
  bootstrap_sha256_env SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARMV7 >/dev/null
  validate_managed_mise_manifest
  bootstrap_pi_version >/dev/null
  bootstrap_herdr_version >/dev/null
  bootstrap_herdr_backend >/dev/null
  bootstrap_sha256_env SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64 >/dev/null
  bootstrap_sha256_env SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64 >/dev/null
  bootstrap_pi_tool >/dev/null
  if [[ ${BOOTSTRAP_TARGET} == all ]]; then
    # Release-backed tools support Linux x86_64/arm64 only. Reject unsupported
    # hosts here, before any install function can mutate the system.
    local row checksum_key
    while IFS= read -r row; do
      IFS='|' read -r _ _ _ _ _ _ _ checksum_key _ <<<"${row}"
      if [[ ${checksum_key} != - ]]; then
        managed_mise_tool_sha256_for_arch "${row}" "$(uname -m)" >/dev/null
      fi
    done <<<"$(managed_mise_tool_rows)"
    bootstrap_herdr_sha256_for_arch "$(uname -m)" >/dev/null
  fi
  local -a packages
  read_package_env SERVERPRO_BOOTSTRAP_GIT_PACKAGES packages
  read_package_env SERVERPRO_BOOTSTRAP_DOCKER_PACKAGES packages
  read_package_env SERVERPRO_BOOTSTRAP_HTOP_PACKAGES packages
}

resolve_target_user() {
  TARGET_USER=${SERVERPRO_BOOTSTRAP_USER:-${SUDO_USER:-}}
  if [[ -z ${TARGET_USER} || ${TARGET_USER} == root ]]; then
    TARGET_USER=
    TARGET_HOME=
    TARGET_GID=
    TARGET_PATH=
    warn 'SERVERPRO_BOOTSTRAP_USER/SUDO_USER is empty or root; skipping per-user mise/node/pi setup'
    return 0
  fi

  validate_user_token SERVERPRO_BOOTSTRAP_USER "${TARGET_USER}"
  local user_record
  if ! user_record=$(getent passwd "${TARGET_USER}"); then
    printf 'target user does not exist: %s\n' "${TARGET_USER}" >&2
    exit 1
  fi

  TARGET_HOME=$(printf '%s' "${user_record}" | cut -d: -f6)
  TARGET_GID=$(printf '%s' "${user_record}" | cut -d: -f4)
  if [[ -z ${TARGET_HOME} || -z ${TARGET_GID} || ! -d ${TARGET_HOME} ]]; then
    printf 'target user home missing: user=%s home=%s\n' "${TARGET_USER}" "${TARGET_HOME}" >&2
    exit 1
  fi
  TARGET_PATH=${TARGET_HOME}/.local/bin:${TARGET_HOME}/.local/share/mise/shims:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
}

require_target_user() {
  if [[ -z ${TARGET_USER:-} || -z ${TARGET_HOME:-} || -z ${TARGET_GID:-} ]]; then
    printf 'target admin user required for user-level bootstrap target: %s\n' "${BOOTSTRAP_TARGET}" >&2
    exit 1
  fi
}

run_as_target() {
  local script=$1
  if [[ -z ${TARGET_USER:-} || -z ${TARGET_HOME:-} ]]; then
    return 0
  fi
  sudo -H -u "${TARGET_USER}" \
    env -i HOME="${TARGET_HOME}" USER="${TARGET_USER}" LOGNAME="${TARGET_USER}" PATH="${TARGET_PATH}" MISE_EXPERIMENTAL=1 \
    bash -c 'cd "$HOME" && exec bash -c "$1"' sh "${script}"
}

version_at_least() {
  local current="$1"
  local minimum="$2"
  [ "$(printf '%s\n%s\n' "${minimum}" "${current}" | sort -V | head -n1)" = "${minimum}" ]
}

mise_binary_bootstrap_capable() {
  local bin="$1"
  local minimum current
  minimum=$(bootstrap_min_mise_version)
  current=$("${bin}" --version)
  # `mise --version` prints `mise <version>[( release-date)]`; keep only the
  # version token before validation and comparison.
  current=${current#mise }
  current=${current%% *}
  validate_version_token mise_version "${current}"
  version_at_least "${current}" "${minimum}" && MISE_EXPERIMENTAL=1 "${bin}" bootstrap --help >/dev/null
}

root_mise_ready() {
  [[ -f ${ROOT_MISE} && ! -L ${ROOT_MISE} && -x ${ROOT_MISE} && -O ${ROOT_MISE} ]] && mise_binary_bootstrap_capable "${ROOT_MISE}"
}

mise_release_arch() {
  case "$(uname -m)" in
    x86_64) printf 'x64' ;;
    aarch64|arm64) printf 'arm64' ;;
    armv7l) printf 'armv7' ;;
    *)
      printf 'unsupported architecture for mise release: %s\n' "$(uname -m)" >&2
      exit 1
      ;;
  esac
}

# fetch_verified_mise_binary downloads the pinned mise GitHub release, verifies
# its SHA-256 against the manifest, extracts only the known mise member, and
# publishes it as a root-owned world-readable temp file. Returning a readable
# path lets the final install into the target user's ~/.local/bin be performed
# BY that user (never root), preserving the "root never writes user-home paths"
# symlink-safety guarantee. Fetch+verify stay off the mutable mise.run installer.
fetch_verified_mise_binary() {
  local version arch base_url filename tmpdir checksum published
  version=$(bootstrap_min_mise_version)
  arch=$(mise_release_arch)
  case "${arch}" in
    x64) checksum=$(bootstrap_sha256_env SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_X64) ;;
    arm64) checksum=$(bootstrap_sha256_env SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARM64) ;;
    armv7) checksum=$(bootstrap_sha256_env SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARMV7) ;;
  esac
  base_url="https://github.com/jdx/mise/releases/download/v${version}"
  filename="mise-v${version}-linux-${arch}.tar.gz"
  tmpdir=$(mktemp -d)
  chmod 0700 "${tmpdir}"

  # Callers use command substitution, so this function runs in a subshell and
  # register_tmp updates would be lost to the parent: one guarded chain cleans
  # the download dir on any download/verify/extract failure. Checksum
  # diagnostics stay on stderr so stdout carries only the published path.
  # --no-same-owner drops packed ownership; extracting only the known member
  # rejects path-traversal entries even inside a checksum-pinned archive.
  if ! {
    curl -fsSL "${base_url}/${filename}" -o "${tmpdir}/${filename}" &&
      printf '%s  %s\n' "${checksum}" "${filename}" >"${tmpdir}/mise.sha256" &&
      (cd "${tmpdir}" && sha256sum -c mise.sha256 >&2) &&
      tar -xzf "${tmpdir}/${filename}" -C "${tmpdir}" --no-same-owner mise/bin/mise
  }; then
    rm -rf "${tmpdir}"
    exit 1
  fi
  # Publish as a root-owned 0755 file the target user can read; the final write
  # into the user's home is done by that user, so root never touches user paths.
  published=$(mktemp)
  install -o root -g root -m 0755 "${tmpdir}/mise/bin/mise" "${published}"
  rm -rf "${tmpdir}"
  printf '%s' "${published}"
}

ensure_root_mise() {
  if root_mise_ready; then
    log "root mise already available: ${ROOT_MISE}"
    return 0
  fi
  if [[ -L ${ROOT_MISE} || ( -e ${ROOT_MISE} && ! -f ${ROOT_MISE} ) ]]; then
    printf 'refusing unsafe root mise path: %s\n' "${ROOT_MISE}" >&2
    exit 1
  fi

  log "installing pinned root mise at ${ROOT_MISE}"
  apt_install ca-certificates curl
  local verified_root_mise
  verified_root_mise=$(fetch_verified_mise_binary)
  # fetch runs under command substitution, so its own register_tmp calls never
  # reach this shell; register the published path here for trap-based cleanup.
  register_tmp "${verified_root_mise}"
  install -o root -g root -m 0755 "${verified_root_mise}" "${ROOT_MISE}"
  if ! root_mise_ready; then
    printf 'mise %s or newer with bootstrap support is required at %s. Found: %s\n' "$(bootstrap_min_mise_version)" "${ROOT_MISE}" "$("${ROOT_MISE}" --version 2>/dev/null || printf 'unavailable')" >&2
    exit 1
  fi
}

install_docker_repo() {
  apt_install ca-certificates curl gnupg
  install -m 0755 -d /etc/apt/keyrings

  local key_tmp
  key_tmp=$(mktemp)
  register_tmp "${key_tmp}"
  # shellcheck source=/dev/null
  . /etc/os-release

  curl -fsSL "https://download.docker.com/linux/${ID}/gpg" -o "${key_tmp}"
  verify_gpg_fingerprint "${key_tmp}" "${DOCKER_GPG_FINGERPRINT}" "docker"
  install -m 0644 -o root -g root "${key_tmp}" /etc/apt/keyrings/docker.asc

  local codename=${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}
  if [[ -z ${codename} ]]; then
    printf 'could not determine Ubuntu/Debian codename for Docker apt source.\n' >&2
    exit 1
  fi

  cat >/etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/${ID}
Suites: ${codename}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
  chmod 0644 /etc/apt/sources.list.d/docker.sources
  APT_UPDATED=0
  MISE_PACKAGE_UPDATED=0
}

ensure_docker_daemon_config() {
  install -d -m 0755 /etc/docker
  if [[ -f /etc/docker/daemon.json ]]; then
    warn '/etc/docker/daemon.json exists; leaving Docker daemon config unchanged'
    return 0
  fi

  local tmp
  tmp=$(mktemp)
  cat >"${tmp}" <<'JSON'
{
  "ip": "127.0.0.1",
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  },
  "live-restore": true,
  "default-address-pools": [
    {
      "base": "172.30.0.0/16",
      "size": 24
    }
  ]
}
JSON
  if atomic_install_file "${tmp}" /etc/docker/daemon.json 0644; then
    DOCKER_CONFIG_CHANGED=1
    log 'installed /etc/docker/daemon.json with loopback port binds, log rotation, live restore, and deterministic address pool'
  fi
}

ufw_active() {
  command_exists ufw && ufw status 2>/dev/null | grep -q '^Status: active'
}

ufw_has_text() {
  ufw status 2>/dev/null | grep -Fq "$1"
}

ufw_allow_out_cidr() {
  local cidr=$1
  local comment=$2
  if ! ufw_active; then
    return 0
  fi
  if ufw_has_text "${cidr}"; then
    return 0
  fi
  ufw allow out to "${cidr}" proto tcp comment "${comment}"
}

ensure_docker_ufw_egress() {
  if ! ufw_active; then
    return 0
  fi

  # Docker loopback publishes are proxied from host loopback to bridge subnets.
  # serverpro strict egress denies that host-to-container hop unless allowed.
  ufw_allow_out_cidr '172.17.0.0/16' 'serverpro docker bridge egress'
  ufw_allow_out_cidr '172.30.0.0/16' 'serverpro docker compose bridge egress'

  if command_exists docker; then
    while IFS= read -r subnet; do
      [[ -n ${subnet} ]] || continue
      ufw_allow_out_cidr "${subnet}" 'serverpro existing docker network egress'
    done < <(
      mapfile -t network_ids < <(docker network ls -q 2>/dev/null || true)
      if [[ ${#network_ids[@]} -gt 0 ]]; then
        docker network inspect "${network_ids[@]}" --format '{{range .IPAM.Config}}{{println .Subnet}}{{end}}'
      fi 2>/dev/null | sort -u || true
    )
  fi
}

bootstrap_package_set() {
  local label="$1"
  local env_name="$2"
  local -a packages
  read_package_env "${env_name}" packages

  ensure_root_mise
  local mise_bin=${ROOT_MISE}
  local -a update_args=()
  if [[ ${MISE_PACKAGE_UPDATED} -eq 0 ]]; then
    update_args=(--update)
    MISE_PACKAGE_UPDATED=1
  fi
  log "converging ${label} packages with mise bootstrap"
  MISE_EXPERIMENTAL=1 MISE_YES=1 "${mise_bin}" --no-config bootstrap packages apply --yes "${update_args[@]}" "${packages[@]}"
  MISE_EXPERIMENTAL=1 MISE_YES=1 "${mise_bin}" --no-config bootstrap packages upgrade --yes "${packages[@]}"
}

install_git() {
  bootstrap_package_set git SERVERPRO_BOOTSTRAP_GIT_PACKAGES
}

install_htop() {
  bootstrap_package_set htop SERVERPRO_BOOTSTRAP_HTOP_PACKAGES
}

remove_docker_conflicts() {
  # Official Docker docs require removing distro packages before using Docker's apt repository.
  DEBIAN_FRONTEND=noninteractive apt-get remove -y \
    docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc \
    >/dev/null 2>&1 || true
}

install_docker() {
  log 'converging Docker Engine from Docker apt repository'
  remove_docker_conflicts
  install_docker_repo
  bootstrap_package_set docker SERVERPRO_BOOTSTRAP_DOCKER_PACKAGES
  ensure_docker_daemon_config
  if [[ ${DOCKER_CONFIG_CHANGED} -eq 1 ]]; then
    dockerd --validate --config-file /etc/docker/daemon.json
    systemctl restart docker
  fi
  systemctl enable --now docker
  ensure_docker_ufw_egress
}

target_mise_ready() {
  local minimum
  minimum=$(bootstrap_min_mise_version)
  run_as_target "set -euo pipefail; bin=\"\$HOME/.local/bin/mise\"; test -f \"\${bin}\" && test ! -L \"\${bin}\" && test -O \"\${bin}\" && test -x \"\${bin}\"; current=\$(\"\${bin}\" --version); current=\${current#mise }; current=\${current%% *}; [[ \$(printf '%s\\n%s\\n' \"${minimum}\" \"\${current}\" | sort -V | head -n1) == \"${minimum}\" ]]; MISE_EXPERIMENTAL=1 \"\${bin}\" bootstrap --help >/dev/null"
}

install_mise() {
  require_target_user
  if target_mise_ready; then
    ensure_mise_shell_activation
    log "mise already installed for ${TARGET_USER}: ${TARGET_HOME}/.local/bin/mise"
    return 0
  fi

  log "installing bootstrap-capable mise for ${TARGET_USER} under ${TARGET_HOME}/.local/bin"
  apt_install ca-certificates curl
  local verified_user_mise
  verified_user_mise=$(fetch_verified_mise_binary)
  # See ensure_root_mise: register the published path in the parent shell.
  register_tmp "${verified_user_mise}"
  run_as_target "set -euo pipefail; install -d -m 0755 \"\$HOME/.local/bin\"; install -m 0755 \"${verified_user_mise}\" \"\$HOME/.local/bin/mise\""
  ensure_mise_shell_activation
  if ! target_mise_ready; then
    printf 'mise %s or newer with bootstrap support is required for %s.\n' "$(bootstrap_min_mise_version)" "${TARGET_USER}" >&2
    exit 1
  fi
}

ensure_mise_shell_activation() {
  if [[ -z ${TARGET_USER:-} || -z ${TARGET_HOME:-} ]]; then
    return 0
  fi

  # Edit user-owned shell files as the target user to avoid root chown races.
  local script
  script=$(cat <<'TARGET_SCRIPT'
set -euo pipefail
bashrc="$HOME/.bashrc"
marker="# serverpro-bootstrap-tools: admin user toolchain"
if [[ -L ${bashrc} || ( -e ${bashrc} && ! -f ${bashrc} ) ]]; then
  printf 'refusing unsafe bashrc path: %s\n' "${bashrc}" >&2
  exit 1
fi
touch "${bashrc}"
chmod 0644 "${bashrc}"
if ! grep -Fq "${marker}" "${bashrc}"; then
  cat >>"${bashrc}" <<'BASHRC'

# serverpro-bootstrap-tools: admin user toolchain
export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:$PATH"
if [ -x "$HOME/.local/bin/mise" ]; then
  eval "$("$HOME/.local/bin/mise" activate bash)"
elif command -v mise >/dev/null 2>&1; then
  eval "$(mise activate bash)"
fi
BASHRC
fi
TARGET_SCRIPT
)
  run_as_target "${script}"
}

ensure_mise_config_file() {
  if [[ -z ${TARGET_USER:-} || -z ${TARGET_HOME:-} ]]; then
    return 0
  fi

  local script
  script=$(cat <<'TARGET_SCRIPT'
set -euo pipefail
config="$HOME/.config/mise/config.toml"
install -d -m 0755 "$(dirname "${config}")"
if [[ -L ${config} ]]; then
  printf 'refusing symlinked mise config: %s\n' "${config}" >&2
  exit 1
fi
if [[ ! -e ${config} ]]; then
  install -m 0600 /dev/null "${config}"
elif [[ ! -f ${config} ]]; then
  printf 'refusing non-regular mise config: %s\n' "${config}" >&2
  exit 1
else
  chmod 0600 "${config}"
fi
TARGET_SCRIPT
)
  run_as_target "${script}"
}

repair_mise_config_for_user() {
  if [[ -z ${TARGET_USER:-} || -z ${TARGET_HOME:-} ]]; then
    return 0
  fi

  local out
  out=$(run_as_target 'MISE_EXPERIMENTAL=1 mise config ls' 2>&1) || true
  if ! printf '%s\n' "${out}" | grep -Fqi 'error parsing config file'; then
    return 0
  fi

  log "repairing unparsable mise config for ${TARGET_USER}"
  local script
  script=$(cat <<'TARGET_SCRIPT'
set -euo pipefail
config="$HOME/.config/mise/config.toml"
install -d -m 0755 "$(dirname "${config}")"
if [[ -L ${config} ]]; then
  printf 'refusing symlinked mise config backup source: %s\n' "${config}" >&2
  exit 1
fi
if [[ -f ${config} ]]; then
  backup_timestamp=$(date -u +%Y%m%d%H%M%S)
  backup=${config}.serverpro-bad-${backup_timestamp}
  mv "${config}" "${backup}"
  chmod 0600 "${backup}"
  printf '[serverpro-bootstrap-tools] warning: moved unparsable mise config to %s\n' "${backup}" >&2
fi
TARGET_SCRIPT
)
  run_as_target "${script}"
  ensure_mise_config_file
}

configure_mise_value_for_target() {
  local key=$1 value=$2
  validate_config_key managed_mise_config_key "${key}"
  run_as_target "set -euo pipefail; config=\"\$HOME/.config/mise/config.toml\"; mise config set --file \"\${config}\" \"${key}\" \"${value}\""
}

remove_legacy_sg_tool_for_target() {
  if [[ -z ${TARGET_USER:-} || -z ${TARGET_HOME:-} ]]; then
    return 0
  fi

  # Retire the active legacy key before configuring ast-grep. `mise unuse`
  # edits TOML through mise itself and prunes the old install when now unused.
  run_as_target 'set -euo pipefail; config="$HOME/.config/mise/config.toml"; if mise config get --file "${config}" tools.sg >/dev/null 2>&1; then mise --yes unuse --path "${config}" sg; fi'
}

configure_managed_mise_tool() {
  local row=$1 version_env alias_key backend_env version_key profile_key profile_env checksum_key
  local version backend profile checksum
  IFS='|' read -r _ version_env alias_key backend_env version_key profile_key profile_env checksum_key _ <<<"${row}"
  version=$(bootstrap_version_env "${version_env}")
  if [[ ${alias_key} != - ]]; then
    backend=$(required_env "${backend_env}")
    configure_mise_value_for_target "${alias_key}" "${backend}"
  fi
  configure_mise_value_for_target "${version_key}" "${version}"
  if [[ ${profile_key} != - ]]; then
    profile=$(required_env "${profile_env}")
    configure_mise_value_for_target "${profile_key}" "${profile}"
  fi
  if [[ ${checksum_key} != - ]]; then
    checksum=$(managed_mise_tool_sha256_for_arch "${row}" "$(uname -m)")
    configure_mise_value_for_target "${checksum_key}" "sha256:${checksum}"
  fi
}

configure_user_tools_for_target() {
  local target=$1 row
  ensure_mise_config_file
  case "${target}" in
    git)
      configure_managed_mise_tool "$(managed_mise_tool_row gh)"
      ;;
    node|pi)
      configure_managed_mise_tool "$(managed_mise_tool_row node)"
      ;;
    all)
      remove_legacy_sg_tool_for_target
      while IFS= read -r row; do
        configure_managed_mise_tool "${row}"
      done <<<"$(managed_mise_tool_rows)"
      configure_mise_value_for_target tool_alias.herdr "$(bootstrap_herdr_backend)"
      configure_mise_value_for_target tools.herdr "$(bootstrap_herdr_version)"
      ;;
  esac
  # Writes run as the target user, preserving ownership without root chown races.
}

managed_mise_probe_script() {
  case "$1" in
    node) cat <<'PROBE'
node_version=$("$mise_bin" exec -- node --version); test "$node_version" = "v$expected_version" || { printf 'expected node %s, got %s\n' "$expected_version" "$node_version" >&2; exit 1; }; npm_version=$("$mise_bin" exec -- npm --version); printf '%s\n%s\n' "$node_version" "$npm_version"
PROBE
      ;;
    uv) cat <<'PROBE'
uv_version=$("$mise_bin" exec -- uv --version); case "$uv_version" in "uv $expected_version"|"uv $expected_version "*) ;; *) printf 'expected uv %s, got %s\n' "$expected_version" "$uv_version" >&2; exit 1 ;; esac; printf '%s\n' "$uv_version"
PROBE
      ;;
    rust) cat <<'PROBE'
rustc_version=$("$mise_bin" exec -- rustc --version); case "$rustc_version" in "rustc $expected_version "*) ;; *) printf 'expected rustc %s, got %s\n' "$expected_version" "$rustc_version" >&2; exit 1 ;; esac; cargo_version=$("$mise_bin" exec -- cargo --version); rustfmt_version=$("$mise_bin" exec -- rustfmt --version); clippy_version=$("$mise_bin" exec -- cargo clippy --version); rust_docs=$("$mise_bin" exec -- rustup component list --installed | grep -m1 '^rust-docs-'); test -n "$rust_docs"; printf '%s\n%s\n%s\n%s\n%s\n' "$rustc_version" "$cargo_version" "$rustfmt_version" "$clippy_version" "$rust_docs"
PROBE
      ;;
    tmux) cat <<'PROBE'
tool_version=$("$mise_bin" exec -- tmux -V); test "$tool_version" = "tmux $expected_version" || { printf 'expected tmux %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"
PROBE
      ;;
    gh) cat <<'PROBE'
tool_version=$("$mise_bin" exec -- gh --version | head -n1); set -- $tool_version; test "${3:-}" = "$expected_version" || { printf 'expected gh %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"
PROBE
      ;;
    rg) cat <<'PROBE'
tool_version=$("$mise_bin" exec -- rg --version | head -n1); case "$tool_version" in "ripgrep $expected_version"|"ripgrep $expected_version "*) ;; *) printf 'expected ripgrep %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1 ;; esac; printf '%s\n' "$tool_version"
PROBE
      ;;
    fd) cat <<'PROBE'
tool_version=$("$mise_bin" exec -- fd --version); test "$tool_version" = "fd $expected_version" || { printf 'expected fd %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"
PROBE
      ;;
    ast-grep) cat <<'PROBE'
tool_version=$("$mise_bin" exec -- ast-grep --version); case "$tool_version" in "ast-grep $expected_version"|"ast-grep $expected_version "*) ;; *) printf 'expected ast-grep %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1 ;; esac; printf '%s\n' "$tool_version"
PROBE
      ;;
    sem) cat <<'PROBE'
tool_version=$("$mise_bin" exec -- sem --version); test "$tool_version" = "sem $expected_version" || { printf 'expected sem %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"
PROBE
      ;;
    inspect)
      # Upstream exposes no version flag, so prove artifact identity before
      # invoking even its help path.
      cat <<'PROBE'
inspect_bin=$("$mise_bin" exec -- sh -c 'command -v inspect'); test -f "$inspect_bin" && test -x "$inspect_bin"; actual_sha=$(sha256sum "$inspect_bin" | awk '{print $1}'); test "$actual_sha" = "$expected_sha" || { printf 'inspect SHA-256 mismatch: expected %s, got %s\n' "$expected_sha" "$actual_sha" >&2; exit 1; }; help=$("$mise_bin" exec -- inspect --help); printf '%s\n' "$help" | grep -Fq 'Entity-level code review'; printf 'inspect %s\nsha256 %s\n' "$expected_version" "$actual_sha"
PROBE
      ;;
    *) printf 'unsupported managed mise probe: %s\n' "$1" >&2; return 1 ;;
  esac
}

target_managed_mise_tool_ready() {
  local row=$1 version_env checksum_key probe version expected_sha= script
  IFS='|' read -r _ version_env _ _ _ _ _ checksum_key _ _ _ probe _ <<<"${row}"
  version=$(bootstrap_version_env "${version_env}")
  if [[ ${checksum_key} != - ]]; then
    expected_sha=$(managed_mise_tool_sha256_for_arch "${row}" "$(uname -m)")
  fi
  script=$(managed_mise_probe_script "${probe}")
  run_as_target "set -euo pipefail; expected_version=\"${version}\"; expected_sha=\"${expected_sha}\"; mise_bin=\"\$HOME/.local/bin/mise\"; ${script}"
}

target_node_ready() {
  target_managed_mise_tool_ready "$(managed_mise_tool_row node)"
}

target_pi_ready() {
  local node_version="$1"
  local expected_pi_version="$2"
  run_as_target "set -euo pipefail; test \"\$(\"\$HOME/.local/bin/mise\" exec -- node --version)\" = \"v${node_version}\"; \"\$HOME/.local/bin/mise\" exec -- npm --version >/dev/null; expected_pi=\"\$HOME/.local/share/mise/installs/node/${node_version}/bin/pi\"; actual_pi=\$(\"\$HOME/.local/bin/mise\" exec -- sh -c 'command -v pi'); test \"\${actual_pi}\" = \"\${expected_pi}\" || { printf 'expected pi at %s, got %s\\n' \"\${expected_pi}\" \"\${actual_pi}\" >&2; exit 1; }; pi_version=\$(\"\$HOME/.local/bin/mise\" exec -- pi --version 2>&1) || { status=\$?; printf 'pi --version failed (%s): %s\\n' \"\${status}\" \"\${pi_version}\" >&2; exit \"\${status}\"; }; test \"\${pi_version}\" = \"${expected_pi_version}\" || { printf 'expected pi %s, got %s\\n' \"${expected_pi_version}\" \"\${pi_version}\" >&2; exit 1; }"
}

# herdr_integrity_script builds the target-user probe that resolves the herdr
# binary, verifies its pinned SHA-256 BEFORE any execution, then checks the
# exact version. Readiness and verification both reuse it so the fail-closed
# digest gate exists in exactly one place.
herdr_integrity_script() {
  local version="$1" expected_sha="$2"
  printf '%s' "set -euo pipefail; herdr_bin=\$(\"\$HOME/.local/bin/mise\" exec -- sh -c 'command -v herdr'); test -f \"\${herdr_bin}\" && test -x \"\${herdr_bin}\"; actual_sha=\$(sha256sum \"\${herdr_bin}\" | awk '{print \$1}'); test \"\${actual_sha}\" = \"${expected_sha}\"; actual=\$(\"\$HOME/.local/bin/mise\" exec -- herdr --version); test \"\${actual}\" = \"herdr ${version}\""
}

target_herdr_ready() {
  local version expected_sha
  version=$(bootstrap_herdr_version)
  expected_sha=$(bootstrap_herdr_sha256_for_arch "$(uname -m)")
  run_as_target "$(herdr_integrity_script "${version}" "${expected_sha}")"
}

target_herdr_pi_integration_ready() {
  run_as_target "set -euo pipefail; \"\$HOME/.local/bin/mise\" exec -- herdr integration status | grep -Fq 'pi: current'"
}

# install_user_tools_for_target probes each managed component and stages only
# the failures, so an aggregate readiness pass would just re-probe everything.
install_user_tools_for_target() {
  local target="$1"
  if [[ -z ${TARGET_USER:-} || -z ${TARGET_HOME:-} ]]; then
    return 0
  fi

  ensure_mise_shell_activation
  repair_mise_config_for_user
  configure_user_tools_for_target "${target}"

  local node_version pi_version herdr_version pi_tool
  node_version=$(bootstrap_node_version)
  pi_version=$(bootstrap_pi_version)
  herdr_version=$(bootstrap_herdr_version)
  pi_tool=$(bootstrap_pi_tool)

  # Probe every managed component and stage only the failures: doctor repair of
  # one stale tool must not reinstall (or re-resolve) the healthy toolchain.
  local -a mise_installs=() force_mise_installs=()
  local install_pi=0 install_herdr=0
  local row key version_env force version
  case "${target}" in
    git)
      row=$(managed_mise_tool_row gh)
      version=$(managed_mise_tool_version gh)
      target_managed_mise_tool_ready "${row}" || mise_installs+=("gh@${version}")
      ;;
    node)
      target_node_ready || mise_installs+=("node@${node_version}")
      ;;
    pi)
      target_node_ready || mise_installs+=("node@${node_version}")
      target_pi_ready "${node_version}" "${pi_version}" || install_pi=1
      ;;
    all)
      while IFS= read -r row; do
        IFS='|' read -r key version_env _ _ _ _ _ _ _ _ force _ <<<"${row}"
        version=$(bootstrap_version_env "${version_env}")
        if target_managed_mise_tool_ready "${row}" >/dev/null; then
          continue
        fi
        if [[ ${force} == true ]]; then
          force_mise_installs+=("${key}@${version}")
        else
          mise_installs+=("${key}@${version}")
        fi
      done <<<"$(managed_mise_tool_rows)"
      target_pi_ready "${node_version}" "${pi_version}" || install_pi=1
      # Force replacement so doctor repair fixes a corrupt exact-version binary.
      target_herdr_ready || install_herdr=1
      ;;
  esac

  local -a staged=()
  if [[ ${#mise_installs[@]} -gt 0 ]]; then
    staged+=("mise --yes install ${mise_installs[*]}")
  fi
  if [[ ${#force_mise_installs[@]} -gt 0 ]]; then
    staged+=("mise --yes install --force ${force_mise_installs[*]}")
  fi
  if [[ ${install_herdr} -eq 1 ]]; then
    staged+=("mise --yes install --force herdr@${herdr_version}")
  fi
  if [[ ${install_pi} -eq 1 ]]; then
    staged+=("\"\$HOME/.local/bin/mise\" exec -- npm install -g ${pi_tool}@${pi_version}")
  fi

  if [[ ${#staged[@]} -gt 0 ]]; then
    local install_sequence
    install_sequence=$(printf '; %s' "${staged[@]}")
    install_sequence=${install_sequence#; }
    # Explicit tool specs keep focused targets from installing unrelated user
    # config tools. Exported npm flags force the npm package manager and disable
    # every npm lifecycle script (install/preinstall/postinstall) so a
    # compromised or buggy package cannot run arbitrary code during
    # `npm install -g` as the admin user; they must be exported, not prefixed,
    # so they also reach the npm command later in the sequence.
    log "installing missing target-user tools for ${TARGET_USER} with scoped mise install"
    run_as_target "set -euo pipefail; export MISE_EXPERIMENTAL=1 MISE_YES=1 MISE_NPM_PACKAGE_MANAGER=npm npm_config_ignore_scripts=true NPM_CONFIG_IGNORE_SCRIPTS=true; ${install_sequence}; mise reshim || true"
  else
    log "target-user mise tools already installed for ${TARGET_USER}"
  fi

  if [[ ${target} == all ]]; then
    if ! target_herdr_ready; then
      printf 'Herdr integrity verification failed before Pi integration.\n' >&2
      exit 1
    fi
    if ! target_herdr_pi_integration_ready; then
      log "installing Herdr Pi integration for ${TARGET_USER}"
      local integration_script
      integration_script=$(cat <<'TARGET_SCRIPT'
set -euo pipefail
install -d -m 0700 "$HOME/.pi/agent"
"$HOME/.local/bin/mise" exec -- herdr integration install pi
TARGET_SCRIPT
)
      run_as_target "${integration_script}"
    fi
  fi
}

verify_git() {
  if [[ -n ${TARGET_USER:-} ]]; then
    run_as_target 'git --version; ssh -V 2>&1 | head -n1'
    return 0
  fi
  git --version
  ssh -V 2>&1 | head -n1
}

verify_docker() {
  docker --version
  docker compose version
  systemctl is-active docker
}

verify_htop() {
  htop --version | head -n1
}

verify_mise() {
  require_target_user
  run_as_target "set -euo pipefail; test -x \"\$HOME/.local/bin/mise\"; mise --version; MISE_EXPERIMENTAL=1 mise bootstrap --help >/dev/null"
}

verify_managed_mise_tool() {
  target_managed_mise_tool_ready "$(managed_mise_tool_row "$1")"
}

verify_node() {
  if [[ -n ${TARGET_USER:-} ]]; then
    verify_managed_mise_tool node
  fi
}

verify_pi() {
  verify_node
  if [[ -n ${TARGET_USER:-} ]]; then
    local node_version pi_version
    node_version=$(bootstrap_node_version)
    pi_version=$(bootstrap_pi_version)
    run_as_target "set -euo pipefail; expected_pi=\"\$HOME/.local/share/mise/installs/node/${node_version}/bin/pi\"; actual_pi=\$(\"\$HOME/.local/bin/mise\" exec -- sh -c 'command -v pi'); test \"\${actual_pi}\" = \"\${expected_pi}\" || { printf 'expected pi at %s, got %s\\n' \"\${expected_pi}\" \"\${actual_pi}\" >&2; exit 1; }; actual=\$(\"\$HOME/.local/bin/mise\" exec -- pi --version 2>&1) || { status=\$?; printf 'pi --version failed (%s): %s\\n' \"\${status}\" \"\${actual}\" >&2; exit \"\${status}\"; }; test \"\${actual}\" = \"${pi_version}\" || { printf 'expected pi %s, got %s\\n' \"${pi_version}\" \"\${actual}\" >&2; exit 1; }; printf '%s\\n' \"\${actual}\""
  fi
}

verify_herdr() {
  if [[ -n ${TARGET_USER:-} ]]; then
    local version expected_sha
    version=$(bootstrap_herdr_version)
    expected_sha=$(bootstrap_herdr_sha256_for_arch "$(uname -m)")
    run_as_target "$(herdr_integrity_script "${version}" "${expected_sha}"); printf '%s\\nsha256 %s\\n' \"\${actual}\" \"\${actual_sha}\"; integration_status=\$(\"\$HOME/.local/bin/mise\" exec -- herdr integration status); printf '%s\\n' \"\${integration_status}\" | grep -Fq 'pi: current'; printf 'pi: current\\n'"
  fi
}

verify_all_user_tools() {
  verify_pi
  if [[ -n ${TARGET_USER:-} ]]; then
    local row key
    while IFS= read -r row; do
      IFS='|' read -r key _ <<<"${row}"
      [[ ${key} == node ]] || target_managed_mise_tool_ready "${row}"
    done <<<"$(managed_mise_tool_rows)"
  fi
  verify_herdr
}

run_bootstrap_target() {
  case "${BOOTSTRAP_TARGET}" in
    all)
      install_mise
      install_git
      install_docker
      install_htop
      install_user_tools_for_target all
      ;;
    git)
      install_mise
      install_git
      install_user_tools_for_target git
      ;;
    docker)
      install_docker
      ;;
    mise)
      install_mise
      ;;
    node)
      install_mise
      install_user_tools_for_target node
      ;;
    pi)
      install_mise
      install_user_tools_for_target pi
      ;;
  esac
}

verify_installation() {
  log 'verification'
  case "${BOOTSTRAP_TARGET}" in
    all)
      verify_git
      verify_docker
      verify_htop
      verify_mise
      verify_all_user_tools
      ;;
    git) verify_git; verify_mise; verify_managed_mise_tool gh ;;
    docker) verify_docker ;;
    mise) verify_mise ;;
    node) verify_mise; verify_node ;;
    pi) verify_mise; verify_pi ;;
  esac
}

main() {
  if [[ $# -gt 1 ]]; then
    printf 'usage: serverpro-bootstrap-tools.sh [all|git|docker|mise|node|pi]\n' >&2
    exit 1
  fi
  BOOTSTRAP_TARGET=${1:-${SERVERPRO_BOOTSTRAP_TARGET:-all}}
  require_root
  require_supported_os
  validate_bootstrap_target
  validate_bootstrap_env
  resolve_target_user

  log "target user: ${TARGET_USER:-<none>}"
  log "target: ${BOOTSTRAP_TARGET}"
  run_bootstrap_target
  verify_installation
  log 'complete'
}

# Guard the top-level invocation so the script can be sourced for unit testing
# (exercising helper functions in isolation) without running the privileged
# installer. Real deliveries never set this, so bootstrap runs unchanged.
if [[ -z ${SERVERPRO_BOOTSTRAP_SOURCE_ONLY:-} ]]; then
  main "$@"
fi
