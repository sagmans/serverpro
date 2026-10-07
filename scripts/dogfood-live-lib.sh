#!/usr/bin/env bash
# WHY file-wide: every constant and context variable here exists for the
# orchestrator and flow files to read, so "assigned but unused" is expected.
# shellcheck disable=SC2034

# Shared run context and helpers for the live dogfood harness, sourced first by
# test-dogfood-live.sh and by each flow file. Flows depend only on this file
# and on the context dogfood_context_init declares, so they can be sourced in
# any order and every helper has exactly one home. Sourcing it again only
# redefines constants and functions.

# DigitalOcean is the default test provider for on-demand dogfood servers. It
# lives here because the prompt flow needs it before the paid flow runs.
DOGFOOD_DEFAULT_PROVIDER="digitalocean"
# Providers covered by the read-only matrix; each needs the defaults below.
DOGFOOD_PROVIDERS="hetzner vultr digitalocean"
DOGFOOD_DEFAULT_HETZNER_LOCATION="fsn1"
DOGFOOD_DEFAULT_HETZNER_SIZE="cx23"
DOGFOOD_DEFAULT_HETZNER_IMAGE="ubuntu-24.04"
DOGFOOD_DEFAULT_VULTR_LOCATION="fra"
DOGFOOD_DEFAULT_VULTR_SIZE="vc2-1c-1gb"
DOGFOOD_DEFAULT_VULTR_IMAGE="1743"
DOGFOOD_DEFAULT_DIGITALOCEAN_LOCATION="fra1"
DOGFOOD_DEFAULT_DIGITALOCEAN_SIZE="s-1vcpu-1gb-amd"
DOGFOOD_DEFAULT_DIGITALOCEAN_IMAGE="ubuntu-24-04-x64"
# Keep mode's persistent home, relative to the operator home, so it never
# shares the operator's real serverpro home.
DOGFOOD_DEFAULT_KEPT_HOME_SUFFIX=".local/state/serverpro-dogfood/home"
# The CLI's fixed per-server state location under a HOME (home, namespace, server).
DOGFOOD_STATE_PATH_FORMAT="%s/.local/state/serverpro/namespaces/%s/servers/%s.json"
# Mirrors config.ValidID so operator-supplied identifiers can never traverse
# out of the isolated HOME before the CLI's own validation would run.
DOGFOOD_VALID_ID_PATTERN='^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$'

# dogfood_context_init is the one place that declares the variables the
# orchestrator and flow files share, so no flow depends on another flow's
# globals or on source order. Args: binary, validator script, work dir.
dogfood_context_init() {
	# Run inputs and artifact paths, fixed for the whole run.
	bin="$1"
	validator_script="$2"
	work_dir="$3"
	out_dir="$work_dir/out"
	results="$work_dir/results.txt"
	home_dir="$work_dir/home"
	operator_home="$HOME"
	# WHY resolved for every run: a throwaway run must see state a kept run left,
	# because both use the same namespace and provider resource names.
	kept_dogfood_home="${SERVERPRO_DOGFOOD_HOME:-$operator_home/$DOGFOOD_DEFAULT_KEPT_HOME_SUFFIX}"

	# Keep mode consequences, derived once so flows branch on intent rather
	# than on the mode: a kept server may predate the run and must survive it
	# (persistent home, reuse, no create requirement); a throwaway server is
	# deleted by the exit trap and must start beside nothing it could collide with.
	server_outlives_run=0
	arm_fallback_delete=1
	require_clean_start=1
	if [[ "${SERVERPRO_DOGFOOD_KEEP_SERVER:-}" == "1" ]]; then
		server_outlives_run=1
		arm_fallback_delete=0
		require_clean_start=0
	fi

	# Result counters and summary lines, written by every case and scenario.
	pass=0
	fail=0
	skip=0
	live=0
	scenario_summary=()

	# Exit-trap inputs: the server to delete as a fallback, and extra teardown
	# hooks that must run before that delete.
	created_namespace=""
	created_server=""
	created_provider=""
	finish_hooks=()

	# Paid-run settings, set by run_destructive_dogfood and read by scenarios
	# and finish hooks.
	provider=""
	namespace=""
	server=""
	admin_user=""
	ingress=""
	create_args=()
	dogfood_scenarios=""
	recovery_timeout=""
	poll_interval=""
	max_age_hours=""
	ssh_timeout=""
	server_ready=0
	# Set once a paid run starts, so the exit trap knows to look for leftovers.
	destructive_started=0
	leftovers_checked=0
}

# log keeps the terminal and the preserved results file in step.
log() {
	printf '%s\n' "$*" | tee -a "$results"
}

# case_path turns a case label into a safe artifact file stem.
case_path() {
	printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '_'
}

# provider_token maps a provider to its dogfood-only token, so the operator's
# production token variables are never read.
provider_token() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_TOKEN:-}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_TOKEN:-}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_TOKEN:-}" ;;
		*) return 1 ;;
	esac
}

# provider_location, provider_size, and provider_image pick the cheapest known
# good catalog entry per provider unless the operator overrides it.
provider_location() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_LOCATION:-$DOGFOOD_DEFAULT_HETZNER_LOCATION}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_LOCATION:-$DOGFOOD_DEFAULT_VULTR_LOCATION}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_LOCATION:-$DOGFOOD_DEFAULT_DIGITALOCEAN_LOCATION}" ;;
		*) return 1 ;;
	esac
}

provider_size() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_SIZE:-$DOGFOOD_DEFAULT_HETZNER_SIZE}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_SIZE:-$DOGFOOD_DEFAULT_VULTR_SIZE}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_SIZE:-$DOGFOOD_DEFAULT_DIGITALOCEAN_SIZE}" ;;
		*) return 1 ;;
	esac
}

provider_image() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_IMAGE:-$DOGFOOD_DEFAULT_HETZNER_IMAGE}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_IMAGE:-$DOGFOOD_DEFAULT_VULTR_IMAGE}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_IMAGE:-$DOGFOOD_DEFAULT_DIGITALOCEAN_IMAGE}" ;;
		*) return 1 ;;
	esac
}

# valid_dogfood_id guards identifiers that become filesystem paths before the
# CLI could reject them.
valid_dogfood_id() {
	local s="$1"
	[[ -n "$s" && "$s" != "." && "$s" != ".." ]] || return 1
	[[ "$s" != */* && "$s" != *\\* ]] || return 1
	[[ "$s" =~ $DOGFOOD_VALID_ID_PATTERN ]] || return 1
}

# env_name_part encodes an identifier the way the CLI names per-server
# environment variables, so the harness exports the exact name it reads.
env_name_part() {
	local input="$1"
	local output=""
	local i char ordinal upper
	LC_CTYPE=C
	for ((i = 0; i < ${#input}; i++)); do
		char="${input:i:1}"
		case "$char" in
			[a-z]) upper="$(printf '%s' "$char" | tr '[:lower:]' '[:upper:]')"; output+="$upper" ;;
			[A-Z0-9]) output+="$char" ;;
			*) printf -v ordinal '%02X' "'$char"; output+="_X${ordinal}_" ;;
		esac
	done
	printf '%s' "$output"
}

# validate_case_output judges a zero-exit result by its JSON contract, because
# exit status alone cannot prove a live operation happened. Identity defaults
# to the current paid-run target.
validate_case_output() {
	local kind="$1"
	local path="$2"
	local expected_provider="${3:-$provider}"
	local expected_namespace="${4:-$namespace}"
	local expected_server="${5:-$server}"
	python3 "$validator_script" "$kind" "$path" "$expected_provider" "$expected_namespace" "$expected_server"
}

# run_case records one judged command, keeping its stdout and stderr as
# artifacts and echoing them on failure so a paid run is diagnosable. Each
# verdict carries the command's wall time so a slow scenario can be traced to
# the step that spent it instead of only the scenario total.
run_case() {
	local expect="$1"
	local validator_kind="$2"
	local label="$3"
	shift 3
	local stem out err status valid started elapsed
	stem="$(case_path "$label")"
	out="$out_dir/$stem.out"
	err="$out_dir/$stem.err"
	started=$SECONDS
	"$@" >"$out" 2>"$err"
	status=$?
	elapsed=$((SECONDS - started))
	valid=1
	if [[ "$expect" == ok && "$status" -eq 0 ]] && ! validate_case_output "$validator_kind" "$out" >>"$err" 2>&1; then
		valid=0
	fi
	if [[ "$expect" == ok && "$status" -eq 0 && "$valid" -eq 1 ]] || [[ "$expect" == fail && "$status" -ne 0 ]]; then
		log "PASS | $label | ${elapsed}s"
		pass=$((pass + 1))
		return 0
	fi
	log "FAIL | $label | exit $status | output-valid=$valid | ${elapsed}s"
	sed 's/^/  stdout: /' "$out" | tee -a "$results"
	sed 's/^/  stderr: /' "$err" | tee -a "$results"
	fail=$((fail + 1))
	return 1
}

# run_live_ok counts passing live cases, so SERVERPRO_REQUIRE_LIVE_DOGFOOD can
# fail a run where every live case was skipped.
run_live_ok() {
	local validator="$1"
	local label="$2"
	shift 2
	if run_case ok "$validator" "$label" "$@"; then
		live=$((live + 1))
		return 0
	fi
	return 1
}

# skip_case records why coverage was not attempted, so a skip stays visible.
skip_case() {
	log "SKIP | $1 | $2"
	skip=$((skip + 1))
}

# positive_int_setting reads a numeric knob and rejects anything that could
# turn a bounded wait into an unbounded one.
positive_int_setting() {
	local name="$1" default="$2" value
	value="${!name:-$default}"
	# Zero would spin against provider APIs, and a leading zero reads as octal.
	if [[ ! "$value" =~ ^[1-9][0-9]*$ ]]; then
		printf 'invalid %s %q: expected a positive integer without leading zeros\n' "$name" "$value" >&2
		exit 2
	fi
	printf '%s' "$value"
}

# run_with_timeout bounds one command. Explicit stdin redirection keeps piped
# input attached, since background jobs otherwise read from /dev/null.
run_with_timeout() {
	local seconds="$1" pid watcher rc
	shift
	"$@" <&0 &
	pid=$!
	# The watcher must not hold stdout, or a $(...) caller waits out the sleep.
	# Children are stopped too, since an ssh child would keep the pipe open.
	(sleep "$seconds" && { pkill -TERM -P "$pid"; kill -TERM "$pid"; }) >/dev/null 2>&1 &
	watcher=$!
	wait "$pid"
	rc=$?
	kill "$watcher" 2>/dev/null
	wait "$watcher" 2>/dev/null
	return "$rc"
}

# state_path locates the current target's state under the given HOME, so
# keep-mode state can be checked from a throwaway run too. Callers always pass
# the HOME so every call site states which home it reads.
state_path() {
	# shellcheck disable=SC2059 # WHY: the format is a named constant, not input.
	printf "$DOGFOOD_STATE_PATH_FORMAT" "$1" "$namespace" "$server"
}

# state_field reads one dotted field from server state; empty when absent.
state_field() {
	python3 "$validator_script" state-field "$(state_path "$HOME")" "$1"
}

# remote_read runs a read-only command on the managed host over Tailscale SSH,
# the same transport serverpro itself uses.
remote_read() {
	local target
	target="$(state_field tailscale.name)"
	[[ -n "$target" ]] || return 1
	run_with_timeout "$ssh_timeout" tailscale ssh "$admin_user@$target" "$1" </dev/null
}

# remote_sudo_script runs the script on stdin as root. The sudo password and
# script travel on stdin, never in argv, mirroring serverpro's remote runner:
# both sudo calls stay children of one shell so the cached credential applies
# without a TTY, and the second runs non-interactively.
remote_sudo_script() {
	local target script
	target="$(state_field tailscale.name)"
	[[ -n "$target" ]] || return 1
	script="$(cat)"
	{
		printf '%s\n' "$SERVERPRO_DOGFOOD_SUDOPASS"
		printf '%s\n' "$script"
	} | run_with_timeout "$ssh_timeout" tailscale ssh "$admin_user@$target" "sh -c 'IFS= read -r p; printf \"%s\\n\" \"\$p\" | sudo -S -p \"\" -v && sudo -n sh -s'"
}
