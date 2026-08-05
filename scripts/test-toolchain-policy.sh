#!/usr/bin/env bash
set -euo pipefail

# One exact patched toolchain prevents broad 1.26.x claims from admitting known-vulnerable patch releases.
readonly GO_VERSION="1.26.5"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ROOT_DIR

cd "${ROOT_DIR}"
grep -Fx "go ${GO_VERSION}" go.mod
grep -Fx "go = \"${GO_VERSION}\"" mise.toml
for workflow in .github/workflows/ci.yml .github/workflows/release.yml; do
  rg -F "${GO_VERSION}" "${workflow}" >/dev/null
done
if rg -n 'Go 1\.26\+|Go 1\.26\.x|go-version: .*1\.26\.x' README.md INSTALLATION.md DEVELOPMENT.md .github/workflows; then
  echo "broad Go 1.26 support claim remains" >&2
  exit 1
fi
grep -F "Go ${GO_VERSION}" DEVELOPMENT.md
grep -F "Go ${GO_VERSION}+" INSTALLATION.md README.md
