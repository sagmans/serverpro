#!/usr/bin/env bash
set -euo pipefail

# Static policy keeps tag-triggered publication on the same tested, immutable path.
readonly GO_VERSION="1.26.5"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ROOT_DIR
readonly WORKFLOW="${ROOT_DIR}/.github/workflows/release.yml"
readonly CI_WORKFLOW="${ROOT_DIR}/.github/workflows/ci.yml"
readonly CHECKOUT_ACTION="actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6.0.2"
readonly SETUP_GO_ACTION="actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0"
readonly FORBIDDEN_RELEASE_MUTATIONS='--clobber|gh release edit|gh release upload'
readonly FORBIDDEN_INLINE_PACKAGING='go build|tar -'

rg -F "go-version: \"${GO_VERSION}\"" "${WORKFLOW}"
rg -F "run: make check" "${WORKFLOW}"
rg -F "bash scripts/package-release.sh" "${WORKFLOW}"
rg -F '"${GITHUB_REF_NAME}" "${GITHUB_SHA}" dist' "${WORKFLOW}"
[[ "$(rg -c 'gh release create' "${WORKFLOW}")" == "1" ]]
for workflow in "${CI_WORKFLOW}" "${WORKFLOW}"; do
  rg -F "${CHECKOUT_ACTION}" "${workflow}"
  rg -F "${SETUP_GO_ACTION}" "${workflow}"
done
if rg -n -- "${FORBIDDEN_RELEASE_MUTATIONS}|${FORBIDDEN_INLINE_PACKAGING}" "${WORKFLOW}"; then
  echo "release workflow bypasses immutable package publication" >&2
  exit 1
fi
