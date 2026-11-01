#!/bin/sh
# Second layer of the GIT_* scrub, sourced by hook scripts before running tools.
#
# .githooks/lib/run-local.sh already unsets the main per-repo variables
# (GIT_DIR, GIT_INDEX_FILE, ...) before any .d script runs; this clears the
# full list git reports. A test fixture that runs
# `git init` or `git config` in a temp dir while one is set would act on this
# repository and could rewrite its identity, signing or core.hooksPath. This
# file keeps that guarantee if a script is ever run outside the hook runner.
# git lists the exact set itself, so this never goes stale. It lives outside
# .githooks so that tree stays an unedited copy of the dotf template. Source
# it with a path relative to the script, for example from .githooks/<hook>.d/:
# . "$(dirname -- "$0")/../../scripts/clean-git-env.sh"
vars=$(git rev-parse --local-env-vars) || exit 1
# shellcheck disable=SC2086 # One variable name per word is intended.
unset -v $vars
unset vars
