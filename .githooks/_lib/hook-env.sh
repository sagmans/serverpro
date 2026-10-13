#!/bin/sh
# Sourced first by every repo hook step. The steps rely on the helpers that
# the hook runner in .githooks/lib exports (staged_git, pushed_shas, at_sha,
# darwin_only); run any other way they must fail loudly rather than check
# the wrong content. Sourced relative to the step, never from the live
# tree, so only the reviewed copy is used.
# shellcheck disable=SC1090 # WHY: the helper path is set by the hook runner.
. "${HOOK_HELPERS:?run from .githooks (scripts/setup-hooks)}"
