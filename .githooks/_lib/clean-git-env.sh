#!/bin/sh
# Sourced by hook steps that run test suites. Git exports repository-local
# variables (GIT_DIR, GIT_INDEX_FILE, ...) to hooks, and test fixtures that
# run `git init` or `git config` in a temp directory would otherwise act on
# this repository instead: a release-contract fixture once rewrote the real
# repo's local identity, signing and core.hooksPath from a hook run. Git
# lists the exact set itself, so nothing here goes stale.
# shellcheck disable=SC2046 # WHY: one variable name per word is intended.
unset $(git rev-parse --local-env-vars)
