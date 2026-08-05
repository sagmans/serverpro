#!/usr/bin/env bash
set -euo pipefail

plan="$(make -n check GO=__SERVERPRO_GO__)"
test_count="$(grep -c '^"__SERVERPRO_GO__" test' <<<"$plan" || true)"
if [[ "$test_count" != '1' ]]; then
	printf 'make check must run one merged Go test invocation, found %s:\n%s\n' "$test_count" "$plan" >&2
	exit 1
fi
for required in '-race' '-covermode=atomic' '-coverprofile="coverage.out"' 'scripts/check-coverage.sh "coverage.out" "81.8"' 'mod tidy -diff'; do
	if ! grep -F -- "$required" <<<"$plan" >/dev/null; then
		printf 'make check missing %s:\n%s\n' "$required" "$plan" >&2
		exit 1
	fi
done
release_plan="$(make -n test-release GO=__SERVERPRO_GO__)"
release_test_count="$(grep -Fc '"__SERVERPRO_GO__" test ./internal/releasecontract' <<<"$release_plan" || true)"
if [[ "$release_test_count" != '1' ]] || ! grep -Fq 'bash scripts/test-release-contract.sh' <<<"$release_plan"; then
	printf 'make test-release must run one focused Go package test and shell contract:\n%s\n' "$release_plan" >&2
	exit 1
fi
root=$PWD
if grep -Fq 'go test ./internal/releasecontract' "$root/scripts/test-release-contract.sh"; then
	printf 'release shell contract must not rerun Go tests already owned by make targets\n' >&2
	exit 1
fi
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/unused"
cat >"$fixture/go.mod" <<'EOF'
module example.com/tidy-check

go 1.26.0

require example.com/unused v0.0.0

replace example.com/unused => ./unused
EOF
: >"$fixture/go.sum"
cat >"$fixture/main.go" <<'EOF'
package main

func main() {}
EOF
cat >"$fixture/unused/go.mod" <<'EOF'
module example.com/unused

go 1.26.0
EOF
cp "$fixture/go.mod" "$fixture/go.mod.before"
cp "$fixture/go.sum" "$fixture/go.sum.before"
if make -s -C "$fixture" -f "$root/Makefile" tidy-check GO="${GO:-go}" >/dev/null 2>&1; then
	printf 'tidy-check unexpectedly accepted module drift\n' >&2
	exit 1
fi
if ! cmp -s "$fixture/go.mod.before" "$fixture/go.mod" || ! cmp -s "$fixture/go.sum.before" "$fixture/go.sum"; then
	printf 'tidy-check rewrote module files while reporting drift\n' >&2
	exit 1
fi
printf 'PASS | make check gate contract\n'
