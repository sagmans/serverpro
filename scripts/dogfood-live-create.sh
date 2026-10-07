#!/usr/bin/env bash
# Paid lifecycle flow sourced by test-dogfood-live.sh. Selected scenarios run
# in one fixed order against one server, so each step starts from the state the
# previous step proved.

DOGFOOD_CREATE_CONFIRMATION="serverpro-live-dogfood"
# DigitalOcean is the default test provider for on-demand dogfood servers.
DOGFOOD_DEFAULT_PROVIDER="digitalocean"
DOGFOOD_DEFAULT_SERVER="web"
# WHY fixed: create adds tailnet policy tag owners and an SSH rule per
# namespace tag, and delete deliberately leaves tailnet-global policy alone.
# A per-run namespace would add new policy entries to the operator's tailnet
# on every run; one stable namespace reuses the same entries.
DOGFOOD_DEFAULT_NAMESPACE="spdogfood"
DOGFOOD_DEFAULT_ADMIN_USER="deploy"
DOGFOOD_DEFAULT_INGRESS="none"
# Execution order is fixed so a scenario never runs before the one it builds on.
DOGFOOD_SCENARIO_ORDER="create status doctor fix bootstrap power import identity delete"
# The default keeps the historical create→status→doctor→bootstrap→delete run.
DOGFOOD_DEFAULT_SCENARIOS="create,status,doctor,bootstrap,delete"
# Scenarios that act on an existing server; without keep mode they need create.
DOGFOOD_SERVER_SCENARIOS="status doctor fix bootstrap power import identity"
# Recovery waits cover provider power transitions and a full reboot.
DOGFOOD_DEFAULT_RECOVERY_TIMEOUT=600
DOGFOOD_DEFAULT_POLL_INTERVAL=15
# A kept server older than this is reported so a forgotten one is noticed.
DOGFOOD_DEFAULT_MAX_AGE_HOURS=24
DOGFOOD_BOOT_ID_PATH="/proc/sys/kernel/random/boot_id"
# A host mid-reboot can leave Tailscale SSH hanging; every remote call is
# bounded so recovery deadlines stay meaningful and a paid run cannot stall.
DOGFOOD_DEFAULT_SSH_TIMEOUT=60
# Set once a paid run starts, so the exit trap knows to look for leftovers.
destructive_started=0
leftovers_checked=0

# scenario_selected reports whether the operator asked for a scenario.
scenario_selected() {
	[[ "$dogfood_scenarios" == *",$1,"* ]]
}

# parse_dogfood_scenarios fails closed on unknown names so a typo cannot
# silently drop coverage from a paid run.
parse_dogfood_scenarios() {
	local raw="${SERVERPRO_DOGFOOD_SCENARIOS:-$DOGFOOD_DEFAULT_SCENARIOS}"
	local item
	local -a items
	IFS=',' read -r -a items <<<"$raw"
	if [[ "${#items[@]}" -eq 0 ]]; then
		printf 'invalid SERVERPRO_DOGFOOD_SCENARIOS %q: expected a comma-separated list from: %s\n' "$raw" "$DOGFOOD_SCENARIO_ORDER" >&2
		exit 2
	fi
	for item in "${items[@]}"; do
		if [[ -z "$item" || " $DOGFOOD_SCENARIO_ORDER " != *" $item "* ]]; then
			printf 'invalid SERVERPRO_DOGFOOD_SCENARIOS entry %q: expected one of: %s\n' "$item" "$DOGFOOD_SCENARIO_ORDER" >&2
			exit 2
		fi
	done
	dogfood_scenarios=",$raw,"
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

# state_path is the CLI's fixed per-server state location under the harness HOME.
state_path() {
	printf '%s/.local/state/serverpro/namespaces/%s/servers/%s.json' "$HOME" "$namespace" "$server"
}

# state_field reads one dotted field from server state; empty when absent.
state_field() {
	python3 - "$(state_path)" "$1" <<'PY'
import json
import sys

path, field = sys.argv[1:3]
try:
    with open(path, encoding="utf-8") as stream:
        value = json.load(stream)
except (OSError, ValueError):
    sys.exit(0)
for part in field.split("."):
    if isinstance(value, list) and part.isdigit() and int(part) < len(value):
        value = value[int(part)]
    elif isinstance(value, dict):
        value = value.get(part)
    else:
        value = None
    if value is None:
        sys.exit(0)
print(value)
PY
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

# wait_live_ok polls a read-only command until its output passes validation or
# the recovery timeout expires, then records one final judged attempt.
wait_live_ok() {
	local validator="$1" label="$2"
	shift 2
	local probe deadline
	probe="$out_dir/$(case_path "$label").probe"
	deadline=$((SECONDS + recovery_timeout))
	while ((SECONDS < deadline)); do
		if "$@" >"$probe" 2>"$probe.err" && validate_case_output "$validator" "$probe" >/dev/null 2>&1; then
			break
		fi
		sleep "$poll_interval"
	done
	run_live_ok "$validator" "$label" "$@"
}

# run_scenario wraps one selected scenario with timing and a result line for
# the end-of-run summary.
run_scenario() {
	local name="$1"
	shift
	scenario_selected "$name" || return 0
	local started=$SECONDS before_pass=$pass before_fail=$fail result rc
	log "SCENARIO | $name | start"
	"$@"
	rc=$?
	if ((fail > before_fail)); then
		result=fail
	elif ((pass > before_pass)); then
		result=pass
	else
		result=skip
	fi
	scenario_summary+=("SCENARIO | $name | $result | $((SECONDS - started))s")
	return "$rc"
}

# list_namespace_servers prints servers that carry this namespace's ownership
# labels at the provider, so leftovers are found even without local state.
list_namespace_servers() {
	local out="$out_dir/discover-$1.json"
	"$bin" --non-interactive -n "$namespace" -p "$provider" server discover >"$out" 2>"$out.err" || return 1
	validate_case_output list "$out" >>"$out.err" 2>&1 || return 1
	python3 - "$out" "$namespace" <<'PY'
import json
import sys

path, namespace = sys.argv[1:3]
with open(path, encoding="utf-8") as stream:
    for candidate in json.load(stream):
        if candidate.get("namespace") == namespace:
            print(candidate["server"])
PY
}

# check_leftovers reports provider servers in the dogfood namespace other than
# the expected one. A throwaway run refuses to start beside leftovers.
check_leftovers() {
	local phase="$1" expected="$2" found others name
	if ! found="$(list_namespace_servers "$phase")"; then
		log "FAIL | live leftover check $phase | discover failed"
		fail=$((fail + 1))
		return 1
	fi
	others=""
	while IFS= read -r name; do
		[[ -n "$name" && "$name" != "$expected" ]] && others+="$name "
	done <<<"$found"
	if [[ -z "$others" ]]; then
		log "PASS | live leftover check $phase"
		pass=$((pass + 1))
		return 0
	fi
	if [[ "$phase" == preflight ]]; then
		log "FAIL | live leftover check $phase | servers already exist in $provider/$namespace: ${others% }"
		fail=$((fail + 1))
		return 1
	fi
	# Provider deletion can lag, so a post-run sighting is reported, not failed.
	log "WARN | live leftover check $phase | servers still listed in $provider/$namespace: ${others% }"
	return 0
}

# check_leftovers_on_exit covers runs that ended early, for example by signal,
# where the normal post-run check never ran and a fallback delete may have
# found nothing to delete.
check_leftovers_on_exit() {
	[[ "$destructive_started" -eq 1 && "$leftovers_checked" -eq 0 ]] || return 0
	local expected=""
	[[ "$keep_server" -eq 1 && -n "$(state_field compute.id)" ]] && expected="$server"
	export SERVERPRO_SERVER_PROVIDER_TOKEN
	SERVERPRO_SERVER_PROVIDER_TOKEN="$(provider_token "$provider")"
	check_leftovers exit "$expected"
	leftovers_checked=1
	unset SERVERPRO_SERVER_PROVIDER_TOKEN
}

# report_server_facts adds identity and age to the summary, and flags a kept
# server old enough to be costing money unnoticed.
report_server_facts() {
	local compute node created age_hours kept=no
	[[ "$keep_server" -eq 1 ]] && kept=yes
	compute="$(state_field compute.id)"
	if [[ -z "$compute" ]]; then
		scenario_summary+=("SERVER | $provider/$namespace/$server | none | kept=$kept")
		return
	fi
	node="$(state_field tailscale.node_id)"
	created="$(state_field created_at)"
	age_hours="$(python3 - "$created" <<'PY'
import sys
from datetime import datetime, timezone

import re

# Go writes nanoseconds, which older Python fromisoformat rejects.
stamp = re.sub(r"(\.\d{6})\d+", r"\1", sys.argv[1]).replace("Z", "+00:00")
try:
    created = datetime.fromisoformat(stamp)
except ValueError:
    sys.exit(0)
print(int((datetime.now(timezone.utc) - created).total_seconds() // 3600))
PY
)"
	scenario_summary+=("SERVER | $provider/$namespace/$server | compute=$compute node=${node:-unknown} age=${age_hours:-unknown}h kept=$kept")
	if [[ "$kept" == yes && -n "$age_hours" ]] && ((age_hours >= max_age_hours)); then
		scenario_summary+=("WARN | kept server age ${age_hours}h reaches SERVERPRO_DOGFOOD_MAX_AGE_HOURS=$max_age_hours; delete it with SERVERPRO_DOGFOOD_SCENARIOS=delete")
	fi
}

scenario_create() {
	if ! run_live_ok doctor-report "live server create" "$bin" "${create_args[@]}"; then
		server_ready=0
		return 1
	fi
	server_ready=1
}

scenario_status() {
	run_live_ok server-status "live server status" "$bin" --non-interactive -n "$namespace" -p "$provider" server status "$server"
}

scenario_doctor() {
	run_live_ok doctor-report "live server doctor" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server"
}

# scenario_fix proves the repair path runs cleanly and leaves a passing host.
scenario_fix() {
	run_live_ok doctor-report "live server doctor --fix" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server" --fix
	run_live_ok doctor-report "live server doctor after fix" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server"
}

scenario_bootstrap() {
	run_live_ok bootstrap-complete "live server bootstrap git" "$bin" --non-interactive -n "$namespace" -p "$provider" server bootstrap "$server" git
}

# scenario_power drives stop, start, and restart, and proves the restart was a
# real reboot by a changed kernel boot ID before trusting the host again.
scenario_power() {
	local -a scope=(-n "$namespace" -p "$provider")
	local boot_before boot_after
	run_live_ok server-status "live server stop" "$bin" --non-interactive --yes "${scope[@]}" server stop "$server" || return 1
	wait_live_ok power-off "live server stopped" "$bin" --non-interactive "${scope[@]}" server status "$server" || return 1
	run_live_ok server-status "live server start" "$bin" --non-interactive --yes "${scope[@]}" server start "$server" || return 1
	wait_live_ok power-on "live server started" "$bin" --non-interactive "${scope[@]}" server status "$server" || return 1
	wait_live_ok doctor-report "live server doctor after start" "$bin" --non-interactive "${scope[@]}" server doctor "$server" || return 1
	if ! boot_before="$(remote_read "cat $DOGFOOD_BOOT_ID_PATH")" || [[ -z "$boot_before" ]]; then
		log "FAIL | live boot id before restart | unreadable"
		fail=$((fail + 1))
		return 1
	fi
	run_live_ok server-status "live server restart" "$bin" --non-interactive --yes "${scope[@]}" server restart "$server" || return 1
	local deadline=$((SECONDS + recovery_timeout))
	boot_after="$boot_before"
	while ((SECONDS < deadline)); do
		boot_after="$(remote_read "cat $DOGFOOD_BOOT_ID_PATH" 2>/dev/null)" || boot_after=""
		[[ -n "$boot_after" && "$boot_after" != "$boot_before" ]] && break
		sleep "$poll_interval"
	done
	if [[ -z "$boot_after" || "$boot_after" == "$boot_before" ]]; then
		log "FAIL | live server rebooted | boot id unchanged within ${recovery_timeout}s"
		fail=$((fail + 1))
		return 1
	fi
	log "PASS | live server rebooted"
	pass=$((pass + 1))
	wait_live_ok doctor-report "live server doctor after restart" "$bin" --non-interactive "${scope[@]}" server doctor "$server"
}

# scenario_import rebuilds local artifacts from provider labels in a separate
# HOME, the operator's recovery path after losing local state, and proves the
# recovered server passes doctor.
scenario_import() {
	local import_home="$work_dir/import-home"
	local -a import_args=(
		-n "$namespace" -p "$provider" --non-interactive --yes server import "$server"
		--admin-user "$admin_user" --tailscale-tailnet "$SERVERPRO_DOGFOOD_TAILNET" --with-tailscale
	)
	if [[ "$ingress" == cloudflare-tunnel ]]; then
		import_args+=(--with-cloudflare --cloudflare-account-id "$SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID")
	fi
	mkdir -p "$import_home" && chmod 700 "$import_home" || return 1
	run_live_ok import-complete "live server import" env HOME="$import_home" "$bin" "${import_args[@]}" || return 1
	run_live_ok doctor-report "live server doctor after import" env HOME="$import_home" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server"
}

scenario_delete() {
	if run_live_ok delete-complete "live server delete" "$bin" --non-interactive --yes -n "$namespace" -p "$provider" server delete "$server"; then
		created_namespace=""
		created_server=""
		created_provider=""
		server_ready=0
	fi
}

run_destructive_dogfood() {
	if [[ "${SERVERPRO_DOGFOOD_CREATE:-}" != "1" ]]; then
		skip_case "live create/delete" "set SERVERPRO_DOGFOOD_CREATE=1 and SERVERPRO_DOGFOOD_CONFIRM=$DOGFOOD_CREATE_CONFIRMATION"
		return
	fi
	if [[ "${SERVERPRO_DOGFOOD_CONFIRM:-}" != "$DOGFOOD_CREATE_CONFIRMATION" ]]; then
		skip_case "live create/delete" "confirmation token missing"
		return
	fi

	local token location size image sudo_env_name scenario
	# Shared with scenario functions and finish hooks through dynamic scope.
	provider="${SERVERPRO_DOGFOOD_PROVIDER:-$DOGFOOD_DEFAULT_PROVIDER}"
	token="$(provider_token "$provider")"
	if [[ -z "$token" ]]; then
		skip_case "live create/delete" "missing provider token for $provider"
		return
	fi
	if [[ -z "${SERVERPRO_DOGFOOD_TAILSCALE_TOKEN:-}" || -z "${SERVERPRO_DOGFOOD_TAILNET:-}" || -z "${SERVERPRO_DOGFOOD_SUDOPASS:-}" ]]; then
		skip_case "live create/delete" "missing Tailscale token, tailnet, or sudo password"
		return
	fi

	namespace="${SERVERPRO_DOGFOOD_NAMESPACE:-$DOGFOOD_DEFAULT_NAMESPACE}"
	server="${SERVERPRO_DOGFOOD_SERVER:-$DOGFOOD_DEFAULT_SERVER}"
	admin_user="${SERVERPRO_DOGFOOD_ADMIN_USER:-$DOGFOOD_DEFAULT_ADMIN_USER}"
	location="${SERVERPRO_DOGFOOD_LOCATION:-}"
	size="${SERVERPRO_DOGFOOD_SIZE:-}"
	image="${SERVERPRO_DOGFOOD_IMAGE:-}"
	[[ -n "$location" ]] || location="$(provider_location "$provider")"
	[[ -n "$size" ]] || size="$(provider_size "$provider")"
	[[ -n "$image" ]] || image="$(provider_image "$provider")"

	# Identifiers feed filesystem paths before the CLI can reject them.
	if ! valid_dogfood_id "$namespace"; then
		printf 'invalid SERVERPRO_DOGFOOD_NAMESPACE %q: must match serverpro identifier grammar\n' "$namespace" >&2
		exit 2
	fi
	if ! valid_dogfood_id "$server"; then
		printf 'invalid SERVERPRO_DOGFOOD_SERVER %q: must match serverpro identifier grammar\n' "$server" >&2
		exit 2
	fi
	if ! valid_dogfood_id "$admin_user"; then
		printf 'invalid SERVERPRO_DOGFOOD_ADMIN_USER %q: must match serverpro identifier grammar\n' "$admin_user" >&2
		exit 2
	fi

	parse_dogfood_scenarios
	recovery_timeout="$(positive_int_setting SERVERPRO_DOGFOOD_RECOVERY_TIMEOUT "$DOGFOOD_DEFAULT_RECOVERY_TIMEOUT")" || exit 2
	poll_interval="$(positive_int_setting SERVERPRO_DOGFOOD_POLL_INTERVAL "$DOGFOOD_DEFAULT_POLL_INTERVAL")" || exit 2
	max_age_hours="$(positive_int_setting SERVERPRO_DOGFOOD_MAX_AGE_HOURS "$DOGFOOD_DEFAULT_MAX_AGE_HOURS")" || exit 2
	ssh_timeout="$(positive_int_setting SERVERPRO_DOGFOOD_SSH_TIMEOUT "$DOGFOOD_DEFAULT_SSH_TIMEOUT")" || exit 2
	# A throwaway server cannot outlive the run, so server scenarios need create.
	if [[ "$keep_server" -ne 1 ]] && ! scenario_selected create; then
		for scenario in $DOGFOOD_SERVER_SCENARIOS; do
			if scenario_selected "$scenario"; then
				printf 'SERVERPRO_DOGFOOD_SCENARIOS includes %s without create; add create or set SERVERPRO_DOGFOOD_KEEP_SERVER=1\n' "$scenario" >&2
				exit 2
			fi
		done
	fi

	sudo_env_name="$(env_name_part "$namespace")_$(env_name_part "$server")_SUDOPASS"
	export SERVERPRO_SERVER_PROVIDER_TOKEN="$token"
	export SERVERPRO_TAILSCALE_TOKEN="$SERVERPRO_DOGFOOD_TAILSCALE_TOKEN"
	export "$sudo_env_name=$SERVERPRO_DOGFOOD_SUDOPASS"
	create_args=(
		-n "$namespace" -p "$provider" --non-interactive --yes server create "$server"
		--location "$location" --size "$size" --image "$image"
		--admin-user "$admin_user" --tailscale-tailnet "$SERVERPRO_DOGFOOD_TAILNET"
		--tailscale-tags "tag:serverpro-$namespace"
	)

	# Unknown ingress values abort instead of silently creating a different server.
	ingress="${SERVERPRO_DOGFOOD_INGRESS:-$DOGFOOD_DEFAULT_INGRESS}"
	case "$ingress" in
		none) ;;
		cloudflare-tunnel)
			if [[ -z "${SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN:-}" || -z "${SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID:-}" ]]; then
				printf 'SERVERPRO_DOGFOOD_INGRESS=cloudflare-tunnel requires SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN and SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID\n' >&2
				exit 2
			fi
			export SERVERPRO_CLOUDFLARE_TOKEN="$SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN"
			create_args+=(--ingress cloudflare-tunnel --cloudflare-account-id "$SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID")
			;;
		*)
			printf 'invalid SERVERPRO_DOGFOOD_INGRESS %q: expected none or cloudflare-tunnel\n' "$ingress" >&2
			exit 2
			;;
	esac

	server_ready=0
	[[ "$keep_server" -eq 1 && -n "$(state_field compute.id)" ]] && server_ready=1
	if [[ "$keep_server" -ne 1 && "$server_ready" -eq 0 ]] && ! check_leftovers preflight ""; then
		skip_case "live create/delete" "leftover servers must be removed first"
	elif ! run_live_ok namespace-created "live namespace create" "$bin" namespace create "$namespace"; then
		skip_case "live create/delete" "namespace create failed"
	elif ! write_credentials "$namespace" "$server" "$token" "$SERVERPRO_DOGFOOD_TAILSCALE_TOKEN" "${SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN:-}"; then
		log "FAIL | live write credentials"
		fail=$((fail + 1))
	else
		# Arm fallback cleanup only for throwaway servers; keep mode leaves the
		# server for later runs until the delete scenario removes it.
		if [[ "$keep_server" -ne 1 ]]; then
			created_namespace="$namespace"
			created_server="$server"
			created_provider="$provider"
		fi
		destructive_started=1
		run_scenario create scenario_create
		if [[ "$server_ready" -eq 1 ]]; then
			run_scenario status scenario_status
			run_scenario doctor scenario_doctor
			run_scenario fix scenario_fix
			run_scenario bootstrap scenario_bootstrap
			run_scenario power scenario_power
			run_scenario import scenario_import
			run_scenario identity scenario_identity
		else
			for scenario in $DOGFOOD_SERVER_SCENARIOS; do
				scenario_selected "$scenario" && skip_case "live $scenario" "no ready server"
			done
		fi
		run_scenario delete scenario_delete
		report_server_facts
		local expected=""
		[[ "$server_ready" -eq 1 ]] && expected="$server"
		check_leftovers after "$expected"
		leftovers_checked=1
	fi
	unset SERVERPRO_SERVER_PROVIDER_TOKEN SERVERPRO_TAILSCALE_TOKEN SERVERPRO_CLOUDFLARE_TOKEN "$sudo_env_name"
}
