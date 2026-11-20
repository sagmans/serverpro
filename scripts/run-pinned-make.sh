#!/usr/bin/env bash
# Run a Makefile target with exact go and uv binaries resolved and verified
# from the current checkout's mise.toml without consulting untrusted worktree
# mise configs. Bypasses any mise make shim so pinned go/uv paths are preserved.
set -euo pipefail

readonly MISE_CONFIG='mise.toml'

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"
mise_config="$repo_root/$MISE_CONFIG"

# shellcheck source=scripts/lib-mise-pin.sh
. "$script_dir/lib-mise-pin.sh"

go_pin="$(parse_mise_pin "$mise_config" go)"
uv_pin="$(parse_mise_pin "$mise_config" uv)"
prepend_pinned_mise_paths "$repo_root" "go@$go_pin" "uv@$uv_pin"

require_pinned_tool go "$go_pin" 'version' 's/^go version go\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p'
require_pinned_tool uv "$uv_pin" '--version' 's/^uv \([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p'

# Bypass any mise make shim, which would otherwise re-invoke mise with global
# config and prepend global go/uv directories ahead of the pins resolved above.
make_bin=""
if command -v mise >/dev/null 2>&1; then
	make_bin="$(mise -C / which make </dev/null 2>/dev/null || true)"
fi
if [ -z "$make_bin" ] || [ ! -x "$make_bin" ]; then
	make_bin=""
	while IFS= read -r candidate; do
		case "$candidate" in
		*/mise/shims/*) continue ;;
		esac
		make_bin="$candidate"
		break
	done < <(type -ap make)
fi
if [ -z "$make_bin" ]; then
	echo "run-pinned-make: no non-shim make found on PATH" >&2
	exit 1
fi

exec "$make_bin" --no-print-directory "$@"
