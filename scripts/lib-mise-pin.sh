#!/usr/bin/env bash
# Shared fail-closed mise.toml pin parser, untrusted-worktree bin-paths
# resolver, and exact version verifier used by scripts/lint-scripts.sh and
# scripts/run-pinned-make.sh.

parse_mise_pin() {
	local mise_config="$1"
	local tool="$2"
	local pin
	if [ -L "$mise_config" ] || [ ! -f "$mise_config" ] || [ ! -s "$mise_config" ]; then
		echo "mise-pin: missing, empty, or symlinked $mise_config" >&2
		exit 1
	fi
	pin="$(awk -v key="$tool" '
		/^[[:space:]]*\[/ { in_tools = ($0 ~ /^[[:space:]]*\[[[:space:]]*tools[[:space:]]*\][[:space:]]*(#.*)?$/); next }
		in_tools {
			line = $0
			sub(/[[:space:]]*#.*/, "", line)
			idx = index(line, "=")
			if (idx == 0) next
			k = substr(line, 1, idx - 1)
			v = substr(line, idx + 1)
			gsub(/^[[:space:]]+|[[:space:]]+$/, "", k)
			gsub(/^"|"$/, "", k)
			gsub(/^[[:space:]]+|[[:space:]]+$/, "", v)
			if (k == key) {
				count++
				if (v ~ /^"[0-9]+\.[0-9]+\.[0-9]+"$/) {
					gsub(/^"|"$/, "", v)
					val = v
				} else {
					bad = 1
				}
			}
		}
		END { if (count == 1 && !bad) print val }
	' "$mise_config")"
	if [ -z "$pin" ]; then
		echo "mise-pin: no exact $tool pin found in [tools] of $mise_config" >&2
		exit 1
	fi
	printf '%s\n' "$pin"
}

# Prepend exact tool@version install dirs without reading $PWD/mise.toml so
# staged snapshots and detached at_sha worktrees succeed under MISE_PARANOID=1.
prepend_pinned_mise_paths() {
	local ignore_root="$1"
	shift
	local pinned_paths=""
	export MISE_IGNORED_CONFIG_PATHS="${MISE_IGNORED_CONFIG_PATHS:+$MISE_IGNORED_CONFIG_PATHS:}$ignore_root"
	command -v mise >/dev/null 2>&1 || return 0
	[ "$#" -gt 0 ] || return 0
	if pinned_paths="$(MISE_NO_CONFIG=1 mise --no-config bin-paths "$@" </dev/null | paste -sd: -)"; then
		if [ -n "$pinned_paths" ]; then
			PATH="$pinned_paths:$PATH"
			export PATH
		fi
	else
		echo "mise-pin: mise bin-paths failed for: $* (run: mise install)" >&2
		exit 1
	fi
}

# Fail closed unless <bin> is installed and reports the exact pinned version.
require_pinned_tool() {
	local bin="$1"
	local expected="$2"
	local version_arg="$3"
	local sed_expr="$4"
	local raw actual
	if ! command -v "$bin" >/dev/null 2>&1; then
		echo "mise-pin: $bin not found (expected $expected; run: mise install)" >&2
		exit 1
	fi
	if ! raw="$("$bin" "$version_arg" </dev/null 2>&1)"; then
		echo "mise-pin: failed to run $bin $version_arg (expected $expected):" >&2
		printf '%s\n' "$raw" >&2
		exit 1
	fi
	actual="$(printf '%s\n' "$raw" | sed -n "$sed_expr" | sed -n '1p')"
	if [ "$actual" != "$expected" ]; then
		echo "mise-pin: $bin version mismatch: expected $expected, got ${actual:-unknown} (run: mise install)" >&2
		exit 1
	fi
}
