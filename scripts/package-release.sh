#!/bin/sh
set -eu

# Explicit archive members prevent ignored workspace files from entering releases.
PROJECT_NAME="serverpro"
MODULE_PATH="github.com/sagmans/serverpro"
DEFAULT_TARGET_LINUX_AMD64="linux/amd64"
DEFAULT_TARGET_LINUX_ARM64="linux/arm64"
DEFAULT_TARGET_DARWIN_AMD64="darwin/amd64"
DEFAULT_TARGET_DARWIN_ARM64="darwin/arm64"
ALLOWED_TARGETS=" ${DEFAULT_TARGET_LINUX_AMD64} ${DEFAULT_TARGET_LINUX_ARM64} ${DEFAULT_TARGET_DARWIN_AMD64} ${DEFAULT_TARGET_DARWIN_ARM64} "
LICENSE_FILE="LICENSE"
NOTICES_FILE="THIRD_PARTY_NOTICES"
HOME_PATH_PATTERN='/Users/[^/[:space:]]+/|/home/[^/[:space:]]+/|[A-Za-z]:\\Users\\[^[:space:]\\]+'
PRIVATE_URL_PATTERN='https?://[^[:space:]]*(\.internal|\.local)([:/]|$)|github\.com/[^/[:space:]]+/[^/[:space:]]*(private|internal)[^[:space:]]*'
CREDENTIAL_PATTERN='AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9_]{36,}|xox[baprs]-[A-Za-z0-9-]{10,}|BEGIN [A-Z ]*PRIVATE KEY'
GO_BIN="${GO:-go}"

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

sha256() {
  shasum -a 256 -- "$1" | awk '{print $1}'
}

verify_archive() {
  archive="$1"
  package_name="$2"
  target="$3"
  verify_dir="${WORK_DIR}/verify/${package_name}"
  expected_manifest="${WORK_DIR}/expected-${package_name}"
  actual_manifest="${WORK_DIR}/actual-${package_name}"

  printf '%s\n' \
    "${package_name}/" \
    "${package_name}/${LICENSE_FILE}" \
    "${package_name}/${NOTICES_FILE}" \
    "${package_name}/${PROJECT_NAME}" | sort >"${expected_manifest}"
  tar -tzf "${archive}" | sort >"${actual_manifest}"
  diff -u "${expected_manifest}" "${actual_manifest}" || fail "unexpected archive manifest: ${archive}"

  mkdir -p "${verify_dir}"
  tar -xzf "${archive}" -C "${verify_dir}"
  extracted="${verify_dir}/${package_name}"
  cmp "${LICENSE_FILE}" "${extracted}/${LICENSE_FILE}" || fail "project license mismatch: ${archive}"
  cmp "${NOTICES_FILE}" "${extracted}/${NOTICES_FILE}" || fail "third-party notices mismatch: ${archive}"

  binary="${extracted}/${PROJECT_NAME}"
  "${GO_BIN}" version -m "${binary}" | grep -F -- '-trimpath=true' >/dev/null || fail "binary lacks trimpath proof: ${archive}"
  if strings "${binary}" | grep -E "${HOME_PATH_PATTERN}|${PRIVATE_URL_PATTERN}|${CREDENTIAL_PATTERN}"; then
    fail "binary contains private path, URL, or credential-like material: ${archive}"
  fi

  host_target="$("${GO_BIN}" env GOOS)/$("${GO_BIN}" env GOARCH)"
  if [ "${target}" = "${host_target}" ]; then
    version_output="$("${binary}" --version)"
    [ "${version_output}" = "${PROJECT_NAME} version ${VERSION}" ] || fail "binary version mismatch: ${version_output}"
  fi
}

[ "$#" -ge 3 ] || fail "usage: $0 VERSION SOURCE_REVISION DIST_DIR [GOOS/GOARCH ...]"
VERSION="$1"
SOURCE_REVISION="$2"
DIST_DIR="$3"
shift 3

printf '%s\n' "${VERSION}" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || fail "invalid release version: ${VERSION}"
printf '%s\n' "${SOURCE_REVISION}" | grep -Eq '^([0-9a-f]{40}|[0-9a-f]{64})$' || fail "invalid source revision: ${SOURCE_REVISION}"
[ -f "${LICENSE_FILE}" ] || fail "missing ${LICENSE_FILE}"
[ -f "${NOTICES_FILE}" ] || fail "missing ${NOTICES_FILE}"
[ ! -e "${DIST_DIR}" ] || fail "release output already exists: ${DIST_DIR}"

if [ "$#" -eq 0 ]; then
  set -- \
    "${DEFAULT_TARGET_LINUX_AMD64}" \
    "${DEFAULT_TARGET_LINUX_ARM64}" \
    "${DEFAULT_TARGET_DARWIN_AMD64}" \
    "${DEFAULT_TARGET_DARWIN_ARM64}"
fi
for target in "$@"; do
  case "${ALLOWED_TARGETS}" in
    *" ${target} "*) ;;
    *) fail "unsupported release target: ${target}" ;;
  esac
done

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/${PROJECT_NAME}-release.XXXXXX")"
trap 'rm -rf "${WORK_DIR}"' EXIT HUP INT TERM
mkdir -p "${DIST_DIR}" "${WORK_DIR}/stage"

for target in "$@"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  package_name="${PROJECT_NAME}-${VERSION}-${goos}-${goarch}"
  package_dir="${WORK_DIR}/stage/${package_name}"
  archive="${DIST_DIR}/${package_name}.tar.gz"
  mkdir -p "${package_dir}"

  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 "${GO_BIN}" build \
    -trimpath \
    -ldflags "-s -w -X ${MODULE_PATH}/internal/cli.Version=${VERSION}" \
    -o "${package_dir}/${PROJECT_NAME}" \
    ./cmd/serverpro
  cp "${LICENSE_FILE}" "${NOTICES_FILE}" "${package_dir}/"
  COPYFILE_DISABLE=1 tar -C "${WORK_DIR}/stage" -czf "${archive}" "${package_name}"
  verify_archive "${archive}" "${package_name}" "${target}"
done

: >"${DIST_DIR}/SHA256SUMS"
: >"${DIST_DIR}/RELEASE_MANIFEST"
printf 'version=%s\nsource_revision=%s\ngo_version=%s\n' \
  "${VERSION}" "${SOURCE_REVISION}" "$("${GO_BIN}" env GOVERSION)" >>"${DIST_DIR}/RELEASE_MANIFEST"
for target in "$@"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  archive_name="${PROJECT_NAME}-${VERSION}-${goos}-${goarch}.tar.gz"
  digest="$(sha256 "${DIST_DIR}/${archive_name}")"
  printf '%s  %s\n' "${digest}" "${archive_name}" >>"${DIST_DIR}/SHA256SUMS"
  printf 'target=%s\narchive=%s sha256=%s\n' "${target}" "${archive_name}" "${digest}" >>"${DIST_DIR}/RELEASE_MANIFEST"
done
