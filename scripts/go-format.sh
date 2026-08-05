#!/usr/bin/env bash
set -euo pipefail

mode="${1:-}"
if [[ "$mode" != "check" && "$mode" != "write" ]]; then
	printf 'usage: %s check|write\n' "$0" >&2
	exit 2
fi

all_files_nul="$(mktemp "${TMPDIR:-/tmp}/serverpro-go-all-files.XXXXXX")"
files_nul="$(mktemp "${TMPDIR:-/tmp}/serverpro-go-files.XXXXXX")"
trap 'rm -f "$all_files_nul" "$files_nul"' EXIT

git ls-files -z --cached --others --exclude-standard '*.go' >"$all_files_nul"
while IFS= read -r -d '' file; do
	[[ -f "$file" ]] && printf '%s\0' "$file"
done <"$all_files_nul" >"$files_nul"

if [[ ! -s "$files_nul" ]]; then
	exit 0
fi

case "$mode" in
write)
	xargs -0 gofmt -w <"$files_nul"
	;;
check)
	files="$(xargs -0 gofmt -l <"$files_nul")"
	if [[ -n "$files" ]]; then
		printf 'Go files need gofmt:\n%s\nRun: make fmt\n' "$files"
		exit 1
	fi
	;;
esac
