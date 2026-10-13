#!/bin/sh
# Sourced by hook steps that run test suites or builds. Second layer behind
# the global dispatcher, which already clears these variables: a release-
# contract fixture once rewrote the real repo's local identity, signing and
# core.hooksPath because a hook leaked GIT_DIR into its temp repo, so this
# repo keeps its own guard in case it ever runs under another hook runner.
# Git lists the exact set itself, so nothing here goes stale. The list is
# read on its own line so a git failure stops the hook instead of skipping
# the scrub.
git_env_vars=$(git rev-parse --local-env-vars) || exit 1
# shellcheck disable=SC2086 # WHY: one variable name per word is intended.
unset -v $git_env_vars
unset -v git_env_vars
