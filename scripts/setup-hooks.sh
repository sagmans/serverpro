#!/usr/bin/env bash
# One-time migration for clones set up with the old per-repo hook router.
#
# The repo's checks now run from the global dotf hooks, which dispatch to
# .githooks/<hook>.d/. Any repository-local or worktree core.hooksPath
# replaces the global hooks entirely, so the sign-off and signing guards
# would silently stop running; the old router in the Git common directory
# is dead weight once that value is gone. This script finds both and, only
# with --apply, removes them. It never writes any other configuration and
# never touches global configuration or the global hooks directory.
#
# Usage: scripts/setup-hooks.sh            # dry run: list what would change
#        scripts/setup-hooks.sh --apply    # remove the listed items
set -euo pipefail

APPLY_FLAG='--apply'
# Name and marker of the v1 router directory written by the old setup.
ROUTER_DIR_NAME='project-hooks'
ROUTER_MARKER='project-dir'
# Every file the v1 router could contain besides per-hook copies of one
# script; anything else means the directory is not ours to delete.
ROUTER_ASSET_HEADER='# Hook router installed in the Git common directory'

apply=false
case "${1:-}" in
"") ;;
"$APPLY_FLAG") apply=true ;;
*)
	echo "usage: $0 [$APPLY_FLAG]" >&2
	exit 2
	;;
esac

# Prints the action, and runs it only in apply mode, so the dry run shows
# exactly the commands the real run executes.
act() {
	if $apply; then
		echo "run: $*"
		"$@"
	else
		echo "would run: $*"
	fi
}

common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
findings=0

# Local scope is shared by every worktree of this clone.
if git config --local --get-all core.hooksPath >/dev/null; then
	echo "local core.hooksPath: $(git config --local --get-all core.hooksPath | tr '\n' ' ')"
	act git config --local --unset-all core.hooksPath
	findings=$((findings + 1))
fi

# Worktree scope exists only with extensions.worktreeConfig; each linked
# worktree has its own file, so every worktree is checked.
while IFS= read -r line; do
	wt=${line#worktree }
	[[ $line == worktree\ * && -d $wt ]] || continue
	if git -C "$wt" config --worktree --get-all core.hooksPath >/dev/null 2>&1 &&
		[[ $(git -C "$wt" config --show-scope --get-all core.hooksPath 2>/dev/null) == worktree* ]]; then
		echo "worktree core.hooksPath in $wt"
		act git -C "$wt" config --worktree --unset-all core.hooksPath
		findings=$((findings + 1))
	fi
done < <(git worktree list --porcelain)

# A value set through include/includeIf lives in another file; editing that
# file is the owner's call, so it is reported, never changed.
included=$(git config --includes --show-origin --show-scope --get-all core.hooksPath 2>/dev/null |
	awk '$1 == "local" || $1 == "worktree"' | grep -v "file:$common_dir/config" || true)
if [[ -n $included ]]; then
	echo "core.hooksPath set from an included file (edit it by hand):"
	echo "$included"
	findings=$((findings + 1))
fi

router_dir="$common_dir/$ROUTER_DIR_NAME"
if [[ -d $router_dir ]]; then
	# Only a directory the old setup wrote is removed: it holds the marker
	# and copies of the router script, nothing else.
	foreign=''
	for f in "$router_dir"/* "$router_dir"/.[!.]*; do
		[[ -e $f ]] || continue
		[[ $(basename -- "$f") == "$ROUTER_MARKER" ]] && continue
		if [[ ! -f $f || -L $f ]] || ! grep -qF "$ROUTER_ASSET_HEADER" "$f"; then
			foreign="$foreign $f"
		fi
	done
	if [[ -n $foreign ]]; then
		echo "keep $router_dir: unexpected entries:$foreign" >&2
		findings=$((findings + 1))
	else
		echo "old router: $router_dir ($(find "$router_dir" -mindepth 1 | wc -l | tr -d ' ') entries)"
		find "$router_dir" -mindepth 1 -print | sed 's/^/  /'
		act rm -r -- "$router_dir"
		findings=$((findings + 1))
	fi
fi

if ((findings == 0)); then
	echo "hooks: nothing to migrate; the global hooks dispatch to .githooks/<hook>.d"
elif ! $apply; then
	echo "dry run only; rerun with $APPLY_FLAG to apply"
fi
echo "next: review and trust this worktree's hooks with githooks-trust"
