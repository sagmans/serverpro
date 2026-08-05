#!/usr/bin/env bash
set -euo pipefail

# Independent assertions keep release packaging changes from weakening their own gate.
readonly PROJECT_NAME="serverpro"
readonly VERSION="v0.0.0"
readonly SOURCE_REVISION="0123456789abcdef0123456789abcdef01234567"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ROOT_DIR
readonly GO_BIN="${GO:-go}"
GOOS="$(${GO_BIN} env GOOS)"
readonly GOOS
GOARCH="$(${GO_BIN} env GOARCH)"
readonly GOARCH
readonly TARGET="${GOOS}/${GOARCH}"
readonly PACKAGE_NAME="${PROJECT_NAME}-${VERSION}-${GOOS}-${GOARCH}"
readonly ARCHIVE_NAME="${PACKAGE_NAME}.tar.gz"
readonly HOME_PATH_PATTERN='/Users/[^/[:space:]]+/|/home/[^/[:space:]]+/|[A-Za-z]:\\Users\\[^[:space:]\\]+'
readonly PRIVATE_URL_PATTERN='https?://[^[:space:]]*(\.internal|\.local)([:/]|$)|github\.com/[^/[:space:]]+/[^/[:space:]]*(private|internal)[^[:space:]]*'
readonly CREDENTIAL_PATTERN='AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9_]{36,}|xox[baprs]-[A-Za-z0-9-]{10,}|BEGIN [A-Z ]*PRIVATE KEY'
readonly DEPENDENCIES=(
  "github.com/inconshreveable/mousetrap@v1.1.0"
  "github.com/spf13/cobra@v1.10.1"
  "github.com/spf13/pflag@v1.0.9"
  "github.com/tailscale/hujson@v0.0.0-20260302212456-ecc657c15afd"
  "golang.org/x/sys@v0.45.0"
  "golang.org/x/term@v0.36.0"
  "gopkg.in/yaml.v3@v3.0.1"
)
TMP_DIR="$(mktemp -d)"
readonly TMP_DIR
readonly DIST_DIR="${TMP_DIR}/dist"
readonly EXTRACT_DIR="${TMP_DIR}/extract"
trap 'rm -rf "${TMP_DIR}"' EXIT

bash "${ROOT_DIR}/scripts/package-release.sh" \
  "${VERSION}" "${SOURCE_REVISION}" "${DIST_DIR}" "${TARGET}"

printf '%s\n' "RELEASE_MANIFEST" "SHA256SUMS" "${ARCHIVE_NAME}" | sort >"${TMP_DIR}/expected-dist"
find "${DIST_DIR}" -mindepth 1 -maxdepth 1 -type f -exec basename {} \; | sort >"${TMP_DIR}/actual-dist"
diff -u "${TMP_DIR}/expected-dist" "${TMP_DIR}/actual-dist"

printf '%s\n' \
  "${PACKAGE_NAME}/" \
  "${PACKAGE_NAME}/LICENSE" \
  "${PACKAGE_NAME}/THIRD_PARTY_NOTICES" \
  "${PACKAGE_NAME}/${PROJECT_NAME}" | sort >"${TMP_DIR}/expected-archive"
tar -tzf "${DIST_DIR}/${ARCHIVE_NAME}" | sort >"${TMP_DIR}/actual-archive"
diff -u "${TMP_DIR}/expected-archive" "${TMP_DIR}/actual-archive"

mkdir -p "${EXTRACT_DIR}"
tar -xzf "${DIST_DIR}/${ARCHIVE_NAME}" -C "${EXTRACT_DIR}"
cmp "${ROOT_DIR}/LICENSE" "${EXTRACT_DIR}/${PACKAGE_NAME}/LICENSE"
cmp "${ROOT_DIR}/THIRD_PARTY_NOTICES" "${EXTRACT_DIR}/${PACKAGE_NAME}/THIRD_PARTY_NOTICES"
for dependency in "${DEPENDENCIES[@]}"; do
  grep -Fx "Dependency: ${dependency}" "${EXTRACT_DIR}/${PACKAGE_NAME}/THIRD_PARTY_NOTICES"
done

readonly BINARY="${EXTRACT_DIR}/${PACKAGE_NAME}/${PROJECT_NAME}"
[[ "$(${BINARY} --version)" == "${PROJECT_NAME} version ${VERSION}" ]]
if strings "${BINARY}" | grep -E "${HOME_PATH_PATTERN}|${PRIVATE_URL_PATTERN}|${CREDENTIAL_PATTERN}"; then
  echo "release binary contains private path, URL, or credential-like material" >&2
  exit 1
fi

(
  cd "${DIST_DIR}"
  shasum -a 256 -c SHA256SUMS
)
grep -Fx "version=${VERSION}" "${DIST_DIR}/RELEASE_MANIFEST"
grep -Fx "source_revision=${SOURCE_REVISION}" "${DIST_DIR}/RELEASE_MANIFEST"
grep -Fx "target=${TARGET}" "${DIST_DIR}/RELEASE_MANIFEST"
grep -F "archive=${ARCHIVE_NAME} sha256=" "${DIST_DIR}/RELEASE_MANIFEST"
