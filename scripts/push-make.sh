#!/bin/sh
# Shared by the Make-based push gates so a range can cross the introduction
# of pinned tools without letting their later removal restore unpinned builds.
# shellcheck source=.githooks/lib/helpers.sh
. "${HOOK_HELPERS:?run from .githooks (scripts/setup-hooks)}"
# shellcheck source=scripts/clean-git-env.sh
. "$(dirname -- "$0")/../../scripts/clean-git-env.sh"

make_at_commit() (
	sha=$1
	shift
	runner=scripts/run-pinned-make.sh
	history=$(git log --full-history -1 --format=%H "$sha" -- "$runner") || return 1
	if [ -n "$history" ]; then
		at_sha "$sha" bash "$runner" "$@"
		return
	fi
	shallow=$(git rev-parse --is-shallow-repository) || return 1
	if [ "$shallow" != false ]; then
		echo >&2 "pre-push: fetch full history to verify the legacy build at $sha"
		return 1
	fi
	at_sha "$sha" make "$@"
)
