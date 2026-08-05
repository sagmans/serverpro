#!/usr/bin/env bash
set -euo pipefail

profile="${1:?coverage profile required}"
minimum="${2:?minimum coverage required}"
go_bin="${GO:-go}"

if [[ ! -f "$profile" ]]; then
	printf 'coverage profile missing: %s\n' "$profile" >&2
	exit 1
fi

report="$("$go_bin" tool cover -func="$profile")"
actual="$(awk '/^total:/ { value=$NF; sub(/%$/, "", value); print value }' <<<"$report")"
if [[ -z "$actual" ]]; then
	printf 'coverage report missing aggregate total\n' >&2
	exit 1
fi
if ! awk -v actual="$actual" -v minimum="$minimum" 'BEGIN { exit !(actual + 0 >= minimum + 0) }'; then
	printf 'aggregate coverage %s%% is below minimum %s%%\n' "$actual" "$minimum" >&2
	exit 1
fi

zero="$(awk '$NF == "0.0%" && $1 != "total:" { print }' <<<"$report")"
if [[ -n "$zero" ]]; then
	printf '0%%-covered function detected:\n%s\n' "$zero" >&2
	exit 1
fi

printf 'coverage policy passed: aggregate=%s%% minimum=%s%%; zero 0%% functions\n' "$actual" "$minimum"
