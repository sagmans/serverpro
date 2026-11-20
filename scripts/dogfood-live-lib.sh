#!/usr/bin/env bash
# WHY file-wide: every constant and context variable here exists for the
# orchestrator and flow files to read, so "assigned but unused" is expected.
# shellcheck disable=SC2034

# Shared run context and helpers for the live dogfood harness, sourced first by
# test-dogfood-live.sh and by each flow file. Flows depend only on this file
# and on the context dogfood_context_init declares, so they can be sourced in
# any order and every helper has exactly one home. Sourcing it again only
# redefines constants and functions.

# Runtime discovery precedes HOME isolation because mise resolves tools per user.
DOGFOOD_PYTHON_REQUEST=(uv run --no-project --python-preference only-managed python)
dogfood_python_init() {
	local selected
	selected=$("${DOGFOOD_PYTHON_REQUEST[@]}" -c 'import sys; print(sys.executable)') || return 1
	if [[ "$selected" != /* || ! -x "$selected" ]]; then
		printf 'managed Python resolution failed: expected an absolute executable path\n' >&2
		return 1
	fi
	DOGFOOD_PYTHON=("$selected")
}
# Vultr is the default test provider for on-demand dogfood servers so a live
# run reuses the provider account that already backs managed servers;
# SERVERPRO_DOGFOOD_PROVIDER picks another. It lives here because the prompt
# flow needs it before the paid flow runs.
DOGFOOD_DEFAULT_PROVIDER="vultr"
# Providers covered by the read-only matrix; each needs the defaults below.
DOGFOOD_PROVIDERS="hetzner vultr digitalocean"
DOGFOOD_DEFAULT_HETZNER_LOCATION="fsn1"
DOGFOOD_DEFAULT_HETZNER_SIZE="cx23"
DOGFOOD_DEFAULT_HETZNER_IMAGE="ubuntu-24.04"
DOGFOOD_DEFAULT_VULTR_LOCATION="fra"
DOGFOOD_DEFAULT_VULTR_SIZE="vc2-1c-1gb"
# Vultr names images by numeric OS ID; 2284 is "Ubuntu 24.04 LTS x64" in the
# live catalog, the only release managed create accepts (1743 is 22.04).
DOGFOOD_DEFAULT_VULTR_IMAGE="2284"
DOGFOOD_DEFAULT_DIGITALOCEAN_LOCATION="fra1"
DOGFOOD_DEFAULT_DIGITALOCEAN_SIZE="s-1vcpu-1gb-amd"
DOGFOOD_DEFAULT_DIGITALOCEAN_IMAGE="ubuntu-24-04-x64"
# Keep mode's persistent home, relative to the operator home, so it never
# shares the operator's real serverpro home.
DOGFOOD_DEFAULT_KEPT_HOME_SUFFIX=".local/state/serverpro-dogfood/home"
# Throwaway run HOMEs live here, relative to the operator home. WHY not the
# temp dir: serverpro keeps a temp-dir HOME away from the OS keyring, so a run
# there would prove only the key file and never the Keychain or Secret Service.
DOGFOOD_RUN_HOME_PARENT_SUFFIX=".cache/serverpro-live-dogfood"
# The key store a paid run must end up using; SERVERPRO_DOGFOOD_KEY_STORE=file
# suits headless hosts with no OS keyring. Each value is a validator kind suffix.
DOGFOOD_DEFAULT_KEY_STORE="keyring"
DOGFOOD_KEY_STORES="keyring file"
# The store for every HOME that must never reach the OS keyring: the run HOME
# before create picks a store, and the import scenario's second HOME.
DOGFOOD_SAFE_KEY_STORE="file"
# serverpro's keyring item names (service, account prefix + config root). The
# harness needs them only to remove a throwaway run's key: the CLI has no key
# delete command, and dead test keys must not pile up in the login keyring.
DOGFOOD_KEYRING_SERVICE="serverpro"
DOGFOOD_KEYRING_ACCOUNT_PREFIX="credential-key:"
DOGFOOD_CREDENTIAL_ROOT_SUFFIX=".config/serverpro"
# serverpro refuses the macOS keyring for a HOME without this folder, because
# security(1) would open a blocking "Keychain Not Found" dialog. A run HOME
# links it to the operator's folder, where the login keychain lives.
DOGFOOD_KEYCHAIN_FOLDER_SUFFIX="Library/Keychains"
# Bounds each keyring cleanup call: security(1) and D-Bus have no deadline of
# their own, and the exit trap must not hang on a stuck keyring.
DOGFOOD_KEYRING_TIMEOUT=30
# The lookup exit status that means "no such item" (security(1) errSecItemNotFound,
# secret-tool's no-match). Any other failure, a timeout included, counts as
# "may still exist", so key removal fails closed.
DOGFOOD_KEYRING_ABSENT_STATUS_DARWIN=44
DOGFOOD_KEYRING_ABSENT_STATUS_LINUX=1
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
	# Set by the orchestrator: create_run_home or the kept home.
	home_dir=""
	operator_home="$HOME"
	# Read once so every keyring and Keychain branch agrees on the OS.
	host_os="$(uname -s)"
	# WHY resolved for every run: a throwaway run must see state a kept run left,
	# because both use the same namespace and provider resource names.
	kept_dogfood_home="${SERVERPRO_DOGFOOD_HOME:-$operator_home/$DOGFOOD_DEFAULT_KEPT_HOME_SUFFIX}"

	# Keep mode consequences, derived once so flows branch on intent rather
	# than on the mode: a kept server may predate the run and must survive it
	# (persistent home, reuse, no create requirement); a throwaway server is
	# deleted by the exit trap and must start beside nothing it could collide with.
	# A throwaway HOME is made per run, proves the key store, and is removed
	# with its key; a kept home may hold a key from an older run, so neither
	# the store check nor key removal applies to it.
	server_outlives_run=0
	arm_fallback_delete=1
	require_clean_start=1
	throwaway_home=1
	if [[ "${SERVERPRO_DOGFOOD_KEEP_SERVER:-}" == "1" ]]; then
		server_outlives_run=1
		arm_fallback_delete=0
		require_clean_start=0
		throwaway_home=0
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
	key_store=""
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
	"${DOGFOOD_PYTHON[@]}" "$validator_script" "$kind" "$path" "$expected_provider" "$expected_namespace" "$expected_server"
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
	# WHY reset signals in both jobs: the exit trap ignores INT/TERM/HUP while
	# it tears down, children inherit that, and the deadline is a TERM, so
	# without the reset neither the command nor the watcher could be stopped.
	(trap - INT TERM HUP; exec "$@") <&0 &
	pid=$!
	# The watcher must not hold stdout, or a $(...) caller waits out the sleep.
	# Children are stopped too, since an ssh child would keep the pipe open.
	(trap - INT TERM HUP; sleep "$seconds" && { pkill -TERM -P "$pid"; kill -TERM "$pid"; }) >/dev/null 2>&1 &
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
	"${DOGFOOD_PYTHON[@]}" "$validator_script" state-field "$(state_path "$HOME")" "$1"
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

# keyring_tool_present reports whether this OS's keyring CLI is installed; the
# harness needs it to prove a run's keyring item is gone.
keyring_tool_present() {
	case "$host_os" in
		Darwin) command -v security >/dev/null 2>&1 ;;
		Linux) command -v secret-tool >/dev/null 2>&1 ;;
		*) return 1 ;;
	esac
}

# key_store_setting reads the key store a paid run must use and fails closed
# on an unknown name, so a typo cannot weaken the check.
key_store_setting() {
	local value="${SERVERPRO_DOGFOOD_KEY_STORE:-$DOGFOOD_DEFAULT_KEY_STORE}"
	if [[ " $DOGFOOD_KEY_STORES " != *" $value "* ]]; then
		printf 'invalid SERVERPRO_DOGFOOD_KEY_STORE %q: expected one of: %s\n' "$value" "$DOGFOOD_KEY_STORES" >&2
		exit 2
	fi
	# Without secret-tool a Linux run could not remove its keyring item, so
	# it refuses to start rather than leave a test key behind.
	if [[ "$throwaway_home" -eq 1 && "$value" == keyring && "$host_os" == Linux ]] && ! keyring_tool_present; then
		printf 'SERVERPRO_DOGFOOD_KEY_STORE=keyring on Linux needs secret-tool to remove the run key; install libsecret-tools or set SERVERPRO_DOGFOOD_KEY_STORE=file\n' >&2
		exit 2
	fi
	printf '%s' "$value"
}

# create_run_home makes the throwaway HOME. Its parent is a physical path so
# home_dir is spelled exactly as serverpro spells the keyring account it
# derives from HOME; a doubled slash would make the key unfindable at exit.
create_run_home() {
	local parent
	parent="$operator_home/$DOGFOOD_RUN_HOME_PARENT_SUFFIX"
	mkdir -p "$parent" && chmod 700 "$parent" || return 1
	parent="$(cd -P -- "$parent" && pwd -P)" || return 1
	home_dir="$(mktemp -d "$parent/home.XXXXXX")" || return 1
	link_keychain_folder
}

# link_keychain_folder lets serverpro reach the operator's login Keychain from
# the run HOME on macOS. Elsewhere, or without that folder, it does nothing.
link_keychain_folder() {
	local source="$operator_home/$DOGFOOD_KEYCHAIN_FOLDER_SUFFIX"
	local link="$home_dir/$DOGFOOD_KEYCHAIN_FOLDER_SUFFIX"
	[[ "$host_os" == Darwin && -d "$source" ]] || return 0
	mkdir -p "$(dirname "$link")" && ln -s "$source" "$link"
}

# run_key_account is the keyring account serverpro uses for the run HOME.
run_key_account() {
	printf '%s%s/%s' "$DOGFOOD_KEYRING_ACCOUNT_PREFIX" "$home_dir" "$DOGFOOD_CREDENTIAL_ROOT_SUFFIX"
}

# run_key_item names the run key as Keychain Access and seahorse show it.
run_key_item() {
	printf '%s/%s' "$DOGFOOD_KEYRING_SERVICE" "$(run_key_account)"
}

# keyring_item removes or looks up the run key's keyring item on this OS.
# stdout is discarded, so a lookup never prints the key; stderr is left to the
# caller, because on Linux it is the only sign that tells an error from a miss.
keyring_item() {
	local action="$1" account
	account="$(run_key_account)"
	case "$host_os:$action" in
		Darwin:find) run_with_timeout "$DOGFOOD_KEYRING_TIMEOUT" security find-generic-password -s "$DOGFOOD_KEYRING_SERVICE" -a "$account" ;;
		Darwin:delete) run_with_timeout "$DOGFOOD_KEYRING_TIMEOUT" security delete-generic-password -s "$DOGFOOD_KEYRING_SERVICE" -a "$account" ;;
		Linux:find) run_with_timeout "$DOGFOOD_KEYRING_TIMEOUT" secret-tool lookup service "$DOGFOOD_KEYRING_SERVICE" username "$account" ;;
		Linux:delete) run_with_timeout "$DOGFOOD_KEYRING_TIMEOUT" secret-tool clear service "$DOGFOOD_KEYRING_SERVICE" username "$account" ;;
		*) return 1 ;;
	esac </dev/null >/dev/null
}

# keyring_item_state reports present, absent, or why the lookup failed. Only
# the OS's own not-found status counts as absent; secret-tool exits 1 for both
# a miss and an error and differs only in printing the error, so on Linux a
# miss must also be silent.
keyring_item_state() {
	local status absent err
	err="$(keyring_item find 2>&1)"
	status=$?
	case "$host_os" in
		Darwin) absent="$DOGFOOD_KEYRING_ABSENT_STATUS_DARWIN" ;;
		*) absent="$DOGFOOD_KEYRING_ABSENT_STATUS_LINUX" ;;
	esac
	if [[ "$status" -eq 0 ]]; then
		printf 'present'
	elif [[ "$status" -eq "$absent" && ( "$host_os" == Darwin || -z "$err" ) ]]; then
		printf 'absent'
	else
		printf 'lookup failed with exit %s' "$status"
	fi
}

# pin_key_store makes serverpro use exactly the given key store in a throwaway
# HOME, so the store a run proves and the store it cleans up are the one it
# chose, not serverpro's automatic pick. Injected keys are dropped because they
# outrank the pin. A kept HOME keeps the operator's own store setup unpinned.
pin_key_store() {
	[[ "$throwaway_home" -eq 1 ]] || return 0
	unset SERVERPRO_CREDENTIAL_KEY SERVERPRO_CREDENTIAL_KEY_COMMAND
	export SERVERPRO_KEY_STORE="$1"
}

# remove_run_key deletes a throwaway run's keyring item and proves it gone.
# Only a run pinned to the keyring can own one; a key file sits in the run
# HOME, which cleanup removes, so file and read-only runs never query a keyring
# that may not work on the host. Fails when the item may remain.
remove_run_key() {
	local item state
	[[ "$key_store" == keyring ]] || return 0
	item="$(run_key_item)"
	case "$host_os" in
		# Without the Keychain link the pinned Keychain refused the key, and a
		# lookup could open the blocking "Keychain Not Found" dialog.
		Darwin) [[ -L "$home_dir/$DOGFOOD_KEYCHAIN_FOLDER_SUFFIX" ]] || return 0 ;;
		Linux) ;;
		*) return 0 ;;
	esac
	# A missing tool means the item cannot be checked, so fail closed.
	if ! keyring_tool_present; then
		log "CLEANUP-FAIL | keyring item $item may remain: no keyring tool"
		return 1
	fi
	state="$(keyring_item_state)"
	[[ "$state" != absent ]] || return 0
	if [[ "$state" == present ]]; then
		keyring_item delete >/dev/null 2>&1
		state="$(keyring_item_state)"
	fi
	if [[ "$state" != absent ]]; then
		log "CLEANUP-FAIL | keyring item $item may remain: $state"
		return 1
	fi
	log "CLEANUP | removed keyring item $item"
}
