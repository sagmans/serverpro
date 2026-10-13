#!/bin/sh
# Hook router installed in the Git common directory, one copy per hook name.
#
# The repository-local core.hooksPath is shared by every worktree, including
# the bare directory of a bare-plus-worktrees layout. A relative path to the
# committed hooks directory would resolve to nothing in a worktree or branch
# that does not contain it yet, and Git would then run no hooks at all, global
# ones included. This router always exists, so each hook run picks the current
# worktree's project hook when present and falls back to the global hook
# otherwise. The global guards are never lost.
#
# Git invokes it by hook name; the name comes from $0.
set -eu

# Written by the setup script; holds the committed hooks directory name
# relative to the worktree root.
PROJECT_DIR_FILE='project-dir'

hook_name=$(basename -- "$0")
router_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
project_dir=$(cat "$router_dir/$PROJECT_DIR_FILE" 2>/dev/null || true)

# Git runs hooks from the worktree root, or from the Git directory in a bare
# repository, where no worktree and no project hooks exist.
top=''
if [ "$(git rev-parse --is-inside-work-tree 2>/dev/null || true)" = true ]; then
	top=$(git rev-parse --show-toplevel)
fi

project_hook="$top/$project_dir/$hook_name"
if [ -n "$top" ] && [ -n "$project_dir" ] && [ -f "$project_hook" ] && [ -x "$project_hook" ]; then
	# The project wrapper runs the global hook itself before project hooks.
	exec "$project_hook" "$@"
fi

# Same precedence and include handling as the project runner.
global_dir=$(git config --global --includes --type=path --get core.hooksPath 2>/dev/null ||
	git config --system --includes --type=path --get core.hooksPath 2>/dev/null ||
	true)
case "$global_dir" in
/* | [A-Za-z]:[/\\]*) ;;
*) exit 0 ;;
esac
# A global path pointing back here would recurse forever.
if [ -d "$global_dir" ] && [ "$(CDPATH='' cd -- "$global_dir" && pwd -P)" = "$router_dir" ]; then
	exit 0
fi
if [ -f "$global_dir/$hook_name" ] && [ -x "$global_dir/$hook_name" ]; then
	exec "$global_dir/$hook_name" "$@"
fi
exit 0
