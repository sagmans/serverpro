#!/bin/sh
# Shared helpers for pre-commit checks. Sourced, not executed: every fast
# check must judge the staged snapshot (what the commit will contain), not
# the working tree, or a partially staged file could pass on unstaged edits.

# Lists staged, non-deleted paths matching the given pathspecs, one per line.
# Deleted files have no content to check, so they are filtered out here once.
staged_files() {
	git diff --cached --name-only --diff-filter=ACMR -- "$@"
}
