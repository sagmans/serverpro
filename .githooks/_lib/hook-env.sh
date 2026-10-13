#!/bin/sh
# Sourced first by every repo hook step. The steps rely on the helpers that
# the global dotf dispatcher provides (staged_git, pushed_shas, at_sha,
# darwin_only); outside that dispatcher they must fail loudly rather than
# check the wrong content. Sourced relative to the step, never from the live
# tree, so only the reviewed copy is used.
# shellcheck disable=SC1090 # WHY: the helper path is set by the dispatcher.
. "${HOOK_HELPERS:?run these hooks through the global dotf dispatcher (see DEVELOPMENT.md)}"
