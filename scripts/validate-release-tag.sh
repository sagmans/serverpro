#!/usr/bin/env bash
set -euo pipefail

SEMVER_PATTERN='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?(\+([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?$'

fail() {
  printf 'invalid release tag: %s\n' "${1:-<missing>}" >&2
  exit 1
}

if [[ $# -ne 1 || ! $1 =~ $SEMVER_PATTERN ]]; then
  fail "${1:-}"
fi

tag=$1
version=${tag#v}
without_build=${version%%+*}
if [[ ${without_build} == *-* ]]; then
  prerelease=${without_build#*-}
  IFS=. read -r -a identifiers <<<"${prerelease}"
  for identifier in "${identifiers[@]}"; do
    # SemVer forbids leading zeroes only for all-numeric prerelease identifiers.
    if [[ ${identifier} =~ ^[0-9]+$ && ${#identifier} -gt 1 && ${identifier} == 0* ]]; then
      fail "${tag}"
    fi
  done
fi
