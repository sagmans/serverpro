#!/usr/bin/env bash
# Installs the repository Git hooks for this clone without hiding the
# operator's global hooks (sign-off, commit format, signature guards).
#
# Git honours a single core.hooksPath and the repository-local value wins,
# so pointing it straight at .githooks would silently disable the global
# hooks. Instead a small router (.githooks/_lib/dispatch.sh) is copied into
# the Git common directory and core.hooksPath points there. The router runs
# this checkout's .githooks/<hook>, whose runner calls the global hook of the
# same name first; checkouts without .githooks fall back to the global hook.
# Only repository-local configuration is written; global configuration and
# the global hooks directory are only read.
set -euo pipefail

HOOKS_DIR='.githooks'
ROUTER_DIR_NAME='project-hooks'
ROUTER_PROJECT_DIR_FILE='project-dir'
ROUTER_ASSET="$HOOKS_DIR/_lib/dispatch.sh"

top=$(git rev-parse --show-toplevel)
cd "$top"
common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
router_dir="$common_dir/$ROUTER_DIR_NAME"

# Same lookup the runner uses; --includes because dotf sets the value in an
# included file, which a plain --global read misses.
global_dir=$(git config --global --includes --type=path --get core.hooksPath 2>/dev/null ||
	git config --system --includes --type=path --get core.hooksPath 2>/dev/null || true)

# Executable hook names in a directory, ignoring Git's *.sample files.
hook_names_in() {
	local dir=$1 f
	[[ -n $dir && -d $dir ]] || return 0
	for f in "$dir"/*; do
		[[ -f $f && -x $f && $f != *.sample ]] && basename -- "$f"
	done
	return 0
}

# The router needs an entry for every hook either side uses; a missing name
# means Git never calls that hook at all, global one included.
mapfile -t names < <({ hook_names_in "$HOOKS_DIR"; hook_names_in "$global_dir"; } | sort -u)

mkdir -p -- "$router_dir"
printf '%s\n' "$HOOKS_DIR" >"$router_dir/$ROUTER_PROJECT_DIR_FILE"
for name in "${names[@]}"; do
	cp -- "$ROUTER_ASSET" "$router_dir/$name"
	chmod +x "$router_dir/$name"
done

# Absolute so every linked worktree resolves the same router.
git config --local core.hooksPath "$router_dir"

echo "hooks: router=$router_dir"
echo "hooks: global=${global_dir:-<none>} ($(hook_names_in "$global_dir" | tr '\n' ' '))"
echo "hooks: routed=${names[*]}"
