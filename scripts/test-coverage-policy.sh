#!/usr/bin/env bash
set -euo pipefail

minimum='81.8'
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
profile="$tmp/coverage.out"
fake_go="$tmp/go"
: >"$profile"
cat >"$fake_go" <<'EOF'
#!/bin/sh
set -eu
[ "$1" = tool ]
[ "$2" = cover ]
printf '%s\n' "$FAKE_COVER_OUTPUT"
EOF
chmod +x "$fake_go"

run_policy() {
	set +e
	POLICY_OUTPUT="$(GO="$fake_go" FAKE_COVER_OUTPUT="$1" bash scripts/check-coverage.sh "$profile" "$minimum" 2>&1)"
	POLICY_STATUS=$?
	set -e
}

run_policy 'example/file.go:1: f 100.0%
total: (statements) 81.8%'
if [[ $POLICY_STATUS -ne 0 ]]; then
	printf 'expected passing coverage policy, got: %s\n' "$POLICY_OUTPUT" >&2
	exit 1
fi

run_policy 'example/file.go:1: f 100.0%
total: (statements) 81.7%'
if [[ $POLICY_STATUS -eq 0 || $POLICY_OUTPUT != *'below minimum 81.8%'* ]]; then
	printf 'expected baseline failure, got: %s\n' "$POLICY_OUTPUT" >&2
	exit 1
fi

run_policy 'example/file.go:1: f 0.0%
total: (statements) 81.8%'
if [[ $POLICY_STATUS -eq 0 || $POLICY_OUTPUT != *'0%-covered function'* ]]; then
	printf 'expected zero-function failure, got: %s\n' "$POLICY_OUTPUT" >&2
	exit 1
fi

printf 'PASS | coverage policy contract\n'
