#!/usr/bin/env bash
# WHY pipefail: harness output flows through tee pipelines; a failing producer
# must not be masked by a succeeding consumer when statuses are inspected.
set -uo pipefail

# Live dogfood harness for serverpro. Read-only provider API checks run when
# tokens are available. Create/delete is disabled unless the caller explicitly
# opts into paid, destructive infrastructure work.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
validator_script="$script_dir/dogfood_validate.py"
lib="$script_dir/dogfood-live-lib.sh"
readonly_flow="$script_dir/dogfood-live-readonly.sh"
destructive_flow="$script_dir/dogfood-live-create.sh"
identity_flow="$script_dir/dogfood-live-identity.sh"
prompt_flow="$script_dir/dogfood-live-prompt.sh"
serverpro_bin="${SERVERPRO_BIN:-${1:-./serverpro}}"
if [[ ! -x "$serverpro_bin" ]]; then
	printf 'serverpro binary not executable: %s\n' "$serverpro_bin" >&2
	exit 2
fi
for required_file in "$validator_script" "$lib" "$readonly_flow" "$destructive_flow" "$identity_flow" "$prompt_flow"; do
	if [[ ! -r "$required_file" ]]; then
		printf 'live dogfood support file not readable: %s\n' "$required_file" >&2
		exit 2
	fi
done

# The lib comes first because it owns the shared context and helpers; flows
# only define their own functions and constants, so their order is free.
# shellcheck source=scripts/dogfood-live-lib.sh
source "$lib"
# shellcheck source=scripts/dogfood-live-readonly.sh
source "$readonly_flow"
# shellcheck source=scripts/dogfood-live-create.sh
source "$destructive_flow"
# shellcheck source=scripts/dogfood-live-identity.sh
source "$identity_flow"
# shellcheck source=scripts/dogfood-live-prompt.sh
source "$prompt_flow"

# WHY reset: bash seeds SECONDS from an inherited environment value, so an
# operator shell that exports it would inflate the reported run duration.
SECONDS=0

dogfood_context_init "$serverpro_bin" "$validator_script" "$(mktemp -d "${TMPDIR:-/tmp}/serverpro-live-dogfood.XXXXXX")"
if [[ "$server_outlives_run" -eq 1 ]]; then
	# WHY a dedicated persistent home: a kept server needs its config, state,
	# and credentials across runs, but must never share the operator's real
	# serverpro home, so production namespaces stay out of reach.
	home_dir="$kept_dogfood_home"
	if [[ "$home_dir" != /* || -L "$home_dir" ]] || ! mkdir -p "$home_dir"; then
		printf 'invalid SERVERPRO_DOGFOOD_HOME %q: must be an absolute, dedicated, non-symlink directory\n' "$home_dir" >&2
		rm -rf "$work_dir"
		exit 2
	fi
	# Compare physical paths so ".", "..", doubled slashes, or a symlinked
	# parent cannot alias the operator's home or any directory above it.
	resolved_home="$(cd -P -- "$home_dir" && pwd -P)"
	resolved_operator="$(cd -P -- "$operator_home" && pwd -P)"
	if [[ -z "$resolved_home" || "$resolved_home" == / || "$resolved_operator/" == "$resolved_home/"* ]] || ! chmod 700 "$resolved_home"; then
		printf 'invalid SERVERPRO_DOGFOOD_HOME %q: must not be the operator home or one of its parents\n' "$home_dir" >&2
		rm -rf "$work_dir"
		exit 2
	fi
	home_dir="$resolved_home"
fi
mkdir -p "$home_dir" "$out_dir"

# cleanup removes the run's temp tree unless the operator asked to inspect it.
cleanup() {
	if [[ "${SERVERPRO_KEEP_HARNESS_TEMP:-}" == "1" ]]; then
		printf 'kept live dogfood temp dir: %s\n' "$work_dir" >&2
		return
	fi
	rm -rf "$work_dir"
}
trap cleanup EXIT

export HOME="$home_dir"

# run_finish_hooks runs teardown that flows registered (for example the
# identity decoy) before the exit trap deletes the server it lives on.
run_finish_hooks() {
	local hook
	for hook in ${finish_hooks[@]+"${finish_hooks[@]}"}; do
		"$hook"
	done
	finish_hooks=()
}

# finish is the single exit path, so teardown order (hooks, server, leftover
# check) holds for success, failure, and interruption alike.
finish() {
	local cleanup_failed=0
	# WHY ignore signals here: a second Ctrl-C during the fallback delete would
	# otherwise abort teardown and leave the paid server running.
	trap '' INT TERM HUP
	run_finish_hooks
	cleanup_created_server || cleanup_failed=1
	check_leftovers_on_exit
	if [[ "$cleanup_failed" -eq 1 ]]; then
		printf 'preserved live dogfood temp dir after cleanup failure: %s\n' "$work_dir" >&2
		printf 'recover manually, then remove: %s\n' "$work_dir" >&2
		exit 1
	fi
	cleanup
}
trap finish EXIT
# WHY explicit signal traps: an interrupted paid run must still reach the
# EXIT trap so the throwaway server and any decoy are removed.
trap 'log "INTERRUPTED | SIGINT"; exit 130' INT
trap 'log "INTERRUPTED | SIGTERM"; exit 143' TERM
trap 'log "INTERRUPTED | SIGHUP"; exit 129' HUP

log "serverpro live dogfood run"
log "binary: $bin"
"$bin" --version | tee -a "$results"
shasum -a 256 "$bin" | tee -a "$results"

# Ask for missing inputs before any API call, so a paid run never starts
# half-configured.
prompt_missing_dogfood_inputs
run_readonly_dogfood
run_destructive_dogfood

if [[ "${SERVERPRO_REQUIRE_LIVE_DOGFOOD:-}" == "1" && "$live" -eq 0 ]]; then
	log "FAIL | live dogfood requirement | no live API case ran"
	fail=$((fail + 1))
fi

for summary_line in ${scenario_summary[@]+"${scenario_summary[@]}"}; do
	log "$summary_line"
done
log "SUMMARY | pass=$pass fail=$fail skip=$skip live=$live duration=${SECONDS}s"
if [[ "$fail" -ne 0 ]]; then
	exit 1
fi
