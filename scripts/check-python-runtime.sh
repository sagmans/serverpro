#!/usr/bin/env bash
# Developer entry points must stop before a system interpreter can run.
set -euo pipefail

UNMANAGED_PATTERN='(^#!.*(env[[:blank:]]+python3|/usr/bin/python3)|(^|[[:blank:]])python3([[:blank:]]|$))'
ROOT="$(cd "${BASH_SOURCE[0]%/*}/.." && pwd)"
cd "$ROOT"
if matches="$(git grep -nE "$UNMANAGED_PATTERN" -- Makefile 'scripts/*.sh' 'scripts/*.py')"; then
	printf 'Python entry point must use the managed runtime:\n%s\n' "$matches" >&2
	exit 1
else
	status=$?
	if [[ "$status" != 1 ]]; then
		printf 'Cannot inspect Python entry points (git grep exit %s)\n' "$status" >&2
		exit "$status"
	fi
fi
printf 'Python entry-point policy passed\n'
