#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VALIDATOR=${ROOT}/scripts/validate-release-tag.sh
CLASSIFIER=${ROOT}/scripts/classify-release-tag.sh
CI_WORKFLOW=${ROOT}/.github/workflows/ci.yml
RELEASE_WORKFLOW=${ROOT}/.github/workflows/release.yml
INSTALLATION_DOC=${ROOT}/INSTALLATION.md
MISE_CONFIG=${ROOT}/mise.toml
GO_VERSION=1.26.5
CHECKOUT_SHA=3d3c42e5aac5ba805825da76410c181273ba90b1
SETUP_GO_SHA=b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
ATTEST_PROVENANCE_SHA=0f67c3f4856b2e3261c31976d6725780e5e4c373
ATTEST_SBOM_SHA=f7c74d28b9d84cb8768d0b8ca14a4bac6ef463e6
SBOM_ACTION_SHA=e22c389904149dbc22b58101806040fa8d37a610

fail() {
  printf 'FAIL | %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local file=$1
  local text=$2
  grep -Fq -- "${text}" "${file}" || fail "${file#"${ROOT}/"} missing: ${text}"
}

assert_absent() {
  local file=$1
  local text=$2
  if grep -Fq -- "${text}" "${file}"; then
    fail "${file#"${ROOT}/"} retains forbidden text: ${text}"
  fi
}

for tag in v0.0.0 v1.2.3 v1.2.3-alpha.1 v1.2.3+build.5 v1.2.3-rc.1+build.5; do
  bash "${VALIDATOR}" "${tag}" || fail "valid release tag rejected: ${tag}"
done
for tag in 1.2.3 v1.2 v01.2.3 v1.02.3 v1.2.03 v1.2.3-01 v1.2.3- v1.2.3+ v1.2.3-! v1.2.3.4; do
  if bash "${VALIDATOR}" "${tag}" >/dev/null 2>&1; then
    fail "invalid release tag accepted: ${tag}"
  fi
done
for case in v1.2.3:false v1.2.3+build.5:false v1.2.3-alpha.1:true v1.2.3-rc.1+build.5:true; do
  tag=${case%%:*}
  expected=${case##*:}
  actual=$(bash "${CLASSIFIER}" "${tag}") || fail "release tag classification failed: ${tag}"
  [[ ${actual} == "${expected}" ]] || fail "release tag ${tag} classified ${actual}; want ${expected}"
done

assert_contains "${MISE_CONFIG}" "go = \"${GO_VERSION}\""
assert_contains "${CI_WORKFLOW}" "workflow_call:"
assert_contains "${CI_WORKFLOW}" "actions/checkout@${CHECKOUT_SHA}"
assert_contains "${CI_WORKFLOW}" "actions/setup-go@${SETUP_GO_SHA}"
assert_contains "${CI_WORKFLOW}" "go-version: \"${GO_VERSION}\""
assert_absent "${CI_WORKFLOW}" "1.26.x"
assert_absent "${CI_WORKFLOW}" "check-latest: true"
assert_contains "${RELEASE_WORKFLOW}" "uses: ./.github/workflows/ci.yml"
assert_contains "${RELEASE_WORKFLOW}" "actions/checkout@${CHECKOUT_SHA}"
assert_contains "${RELEASE_WORKFLOW}" "actions/setup-go@${SETUP_GO_SHA}"
assert_contains "${RELEASE_WORKFLOW}" "go-version: \"${GO_VERSION}\""
assert_contains "${RELEASE_WORKFLOW}" "bash scripts/validate-release-tag.sh \"\${GITHUB_REF_NAME}\""
assert_contains "${RELEASE_WORKFLOW}" "gh release view \"\${GITHUB_REF_NAME}\""
assert_contains "${RELEASE_WORKFLOW}" "macos-15-intel"
assert_contains "${RELEASE_WORKFLOW}" "macos-15"
assert_contains "${RELEASE_WORKFLOW}" "./serverpro --version"
assert_contains "${RELEASE_WORKFLOW}" "anchore/sbom-action@${SBOM_ACTION_SHA}"
assert_contains "${RELEASE_WORKFLOW}" "actions/attest-build-provenance@${ATTEST_PROVENANCE_SHA}"
assert_contains "${RELEASE_WORKFLOW}" "actions/attest@${ATTEST_SBOM_SHA}"
assert_contains "${RELEASE_WORKFLOW}" "gh release create"
assert_absent "${RELEASE_WORKFLOW}" "gh release edit"
assert_absent "${RELEASE_WORKFLOW}" "--clobber"
assert_absent "${RELEASE_WORKFLOW}" "1.26.x"
assert_absent "${RELEASE_WORKFLOW}" "check-latest: true"
assert_contains "${INSTALLATION_DOC}" "gh release download \"\${version}\""
assert_contains "${INSTALLATION_DOC}" "grep -F \"  \${archive}\" SHA256SUMS"
assert_contains "${INSTALLATION_DOC}" "--bundle \"\${provenance_bundle}\""
assert_contains "${INSTALLATION_DOC}" '--predicate-type https://spdx.dev/Document/v2.3'

printf 'PASS | release contract\n'
