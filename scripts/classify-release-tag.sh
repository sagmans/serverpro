#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
bash "${ROOT}/scripts/validate-release-tag.sh" "$@"

version=${1#v}
without_build=${version%%+*}
if [[ ${without_build} == *-* ]]; then
  printf 'true\n'
else
  printf 'false\n'
fi
