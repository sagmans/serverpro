#!/usr/bin/env bash
# Paid lifecycle flow sourced by test-dogfood-live.sh. Selected scenarios run
# in one fixed order against one server, so each step starts from the state the
# previous step proved.

# The lib is this flow's only dependency; sourcing it here keeps that explicit
# and lets shellcheck lint the flow alone. Re-sourcing only redefines.
# shellcheck source=scripts/dogfood-live-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/dogfood-live-lib.sh"

DOGFOOD_DEFAULT_SERVER="web"
# WHY fixed: create adds tailnet policy tag owners and an SSH rule per
# namespace tag, and delete deliberately leaves tailnet-global policy alone.
# A per-run namespace would add new policy entries to the operator's tailnet
# on every run; one stable namespace reuses the same entries.
DOGFOOD_DEFAULT_NAMESPACE="spdogfood"
DOGFOOD_DEFAULT_ADMIN_USER="deploy"
DOGFOOD_DEFAULT_INGRESS="none"
# Execution order is fixed so a scenario never runs before the one it builds
# on. Each name maps to a scenario_<name> function; adding one is one edit here.
DOGFOOD_SCENARIO_ORDER="create status doctor fix bootstrap power import identity delete"
# Scenarios that do not need an existing server; every other one does, so
# without keep mode it needs create in the same run.
DOGFOOD_LIFECYCLE_SCENARIOS="create delete"
# The default keeps the historical create→status→doctor→bootstrap→delete run.
DOGFOOD_DEFAULT_SCENARIOS="create,status,doctor,bootstrap,delete"
# Recovery waits cover provider power transitions and a full reboot.
DOGFOOD_DEFAULT_RECOVERY_TIMEOUT=600
DOGFOOD_DEFAULT_POLL_INTERVAL=15
# A kept server older than this is reported so a forgotten one is noticed.
DOGFOOD_DEFAULT_MAX_AGE_HOURS=24
DOGFOOD_BOOT_ID_PATH="/proc/sys/kernel/random/boot_id"
# A host mid-reboot can leave Tailscale SSH hanging; every remote call is
# bounded so recovery deadlines stay meaningful and a paid run cannot stall.
DOGFOOD_DEFAULT_SSH_TIMEOUT=60

# scenario_selected reports whether the operator asked for a scenario.
scenario_selected() {
	[[ "$dogfood_scenarios" == *",$1,"* ]]
}

# server_scenario reports whether a scenario acts on an existing server, so
# it is skipped when no server is ready.
server_scenario() {
	[[ " $DOGFOOD_LIFECYCLE_SCENARIOS " != *" $1 "* ]]
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

# kept_state_conflicts reports whether keep-mode state still tracks this
# server. Its provider resources share names with a throwaway run, so a
# throwaway create would collide with them instead of failing cleanly.
kept_state_conflicts() {
	[[ "$require_clean_start" -eq 1 && -e "$(state_path "$kept_dogfood_home")" ]]
}

# write_credentials gives the CLI its per-server credential file. Tokens go
# through the environment, never argv, because process argument lists are
# world-readable on shared hosts; only non-secret identifiers stay in argv.
write_credentials() {
	local namespace="$1"
	local server="$2"
	local cred_dir="$HOME/.config/serverpro/namespaces/$namespace/servers/$server"
	mkdir -p "$cred_dir" || return
	chmod 700 "$HOME/.config/serverpro" "$HOME/.config/serverpro/namespaces" "$HOME/.config/serverpro/namespaces/$namespace" "$HOME/.config/serverpro/namespaces/$namespace/servers" "$cred_dir" 2>/dev/null || true
	PROVIDER_TOKEN="$3" TAILSCALE_TOKEN="$4" CLOUDFLARE_TOKEN="$5" \
		python3 "$validator_script" write-credentials "$cred_dir/credentials.json" "$namespace" "$server"
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

# run_selected_scenarios walks the fixed order once, so selection, ordering,
# and the no-ready-server skip all come from DOGFOOD_SCENARIO_ORDER.
run_selected_scenarios() {
	local scenario
	for scenario in $DOGFOOD_SCENARIO_ORDER; do
		if server_scenario "$scenario" && [[ "$server_ready" -ne 1 ]]; then
			scenario_selected "$scenario" && skip_case "live $scenario" "no ready server"
			continue
		fi
		run_scenario "$scenario" "scenario_$scenario"
	done
}

# list_namespace_servers prints servers that carry this namespace's ownership
# labels at the provider, so leftovers are found even without local state.
list_namespace_servers() {
	local out="$out_dir/discover-$1.json"
	"$bin" --non-interactive -n "$namespace" -p "$provider" server discover >"$out" 2>"$out.err" || return 1
	validate_case_output list "$out" >>"$out.err" 2>&1 || return 1
	python3 "$validator_script" namespace-servers "$out" "$namespace"
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

# cleanup_created_server is the fallback delete for a throwaway server the run
# armed but did not delete itself, for example after a failure or signal. It
# fails only when paid resources may remain, so the caller preserves evidence.
cleanup_created_server() {
	if [[ -z "$created_namespace" || -z "$created_server" || -z "$created_provider" ]]; then
		return 0
	fi
	log "CLEANUP | deleting $created_provider/$created_namespace/$created_server"
	# WHY no error suppression: a failed fallback delete must stay loud so the
	# operator can recover paid resources from the preserved artifacts.
	if SERVERPRO_SERVER_PROVIDER_TOKEN="$(provider_token "$created_provider")" \
		"$bin" -n "$created_namespace" -p "$created_provider" --yes server delete "$created_server" \
		>"$out_dir/cleanup-delete.out" 2>"$out_dir/cleanup-delete.err" && \
		validate_case_output delete-complete "$out_dir/cleanup-delete.out" "$created_provider" "$created_namespace" "$created_server" \
		>>"$out_dir/cleanup-delete.err" 2>&1; then
		log "CLEANUP | deleted $created_provider/$created_namespace/$created_server"
		return 0
	fi
	log "CLEANUP-FAIL | delete failed or returned invalid evidence | resources may remain: $created_provider/$created_namespace/$created_server"
	return 1
}

# check_leftovers_on_exit covers runs that ended early, for example by signal,
# where the normal post-run check never ran and a fallback delete may have
# found nothing to delete.
check_leftovers_on_exit() {
	[[ "$destructive_started" -eq 1 && "$leftovers_checked" -eq 0 ]] || return 0
	local expected=""
	[[ "$server_outlives_run" -eq 1 && -n "$(state_field compute.id)" ]] && expected="$server"
	export SERVERPRO_SERVER_PROVIDER_TOKEN
	SERVERPRO_SERVER_PROVIDER_TOKEN="$(provider_token "$provider")"
	check_leftovers exit "$expected"
	leftovers_checked=1
	unset SERVERPRO_SERVER_PROVIDER_TOKEN
}

# report_server_facts adds identity and age to the summary, and flags a kept
# server old enough to be costing money unnoticed.
report_server_facts() {
	local compute node age_hours kept=no
	[[ "$server_outlives_run" -eq 1 ]] && kept=yes
	compute="$(state_field compute.id)"
	if [[ -z "$compute" ]]; then
		# Without compute nothing billable is kept, even in keep mode.
		scenario_summary+=("SERVER | $provider/$namespace/$server | compute=none kept=no")
		return
	fi
	node="$(state_field tailscale.node_id)"
	age_hours="$(python3 "$validator_script" age-hours "$(state_field created_at)")"
	scenario_summary+=("SERVER | $provider/$namespace/$server | compute=$compute node=${node:-unknown} age=${age_hours:-unknown}h kept=$kept")
	if [[ "$kept" == yes && -n "$age_hours" ]] && ((age_hours >= max_age_hours)); then
		scenario_summary+=("WARN | kept server age ${age_hours}h reaches SERVERPRO_DOGFOOD_MAX_AGE_HOURS=$max_age_hours; delete it with SERVERPRO_DOGFOOD_SCENARIOS=delete")
	fi
}

# scenario_create marks the server ready only after create proved it, so
# server scenarios never run against a half-built host.
scenario_create() {
	if ! run_live_ok doctor-report "live server create" "$bin" "${create_args[@]}"; then
		server_ready=0
		return 1
	fi
	server_ready=1
}

# scenario_status proves the status row reports the requested identity.
scenario_status() {
	run_live_ok server-status "live server status" "$bin" --non-interactive -n "$namespace" -p "$provider" server status "$server"
}

# scenario_doctor proves a fresh or kept host passes every doctor check.
scenario_doctor() {
	run_live_ok doctor-report "live server doctor" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server"
}

# scenario_fix proves the repair path runs cleanly and leaves a passing host.
scenario_fix() {
	run_live_ok doctor-report "live server doctor --fix" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server" --fix
	run_live_ok doctor-report "live server doctor after fix" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server"
}

# scenario_bootstrap proves a bootstrap rerun on an existing host is idempotent.
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

# scenario_delete disarms the fallback delete once the server is proven gone,
# so the exit trap does not delete it a second time.
scenario_delete() {
	if run_live_ok delete-complete "live server delete" "$bin" --non-interactive --yes -n "$namespace" -p "$provider" server delete "$server"; then
		created_namespace=""
		created_server=""
		created_provider=""
		server_ready=0
	fi
}

# run_destructive_dogfood validates every paid-run input before the first
# mutating call, then runs the selected scenarios against one server.
run_destructive_dogfood() {
	if [[ "${SERVERPRO_DOGFOOD_CREATE:-}" != "1" ]]; then
		skip_case "live create/delete" "set SERVERPRO_DOGFOOD_CREATE=1"
		return
	fi

	local token location size image sudo_env_name scenario expected
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
	if [[ "$server_outlives_run" -ne 1 ]] && ! scenario_selected create; then
		for scenario in $DOGFOOD_SCENARIO_ORDER; do
			if server_scenario "$scenario" && scenario_selected "$scenario"; then
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
	[[ "$server_outlives_run" -eq 1 && -n "$(state_field compute.id)" ]] && server_ready=1
	if kept_state_conflicts; then
		log "FAIL | live kept state check | $kept_dogfood_home tracks $provider/$namespace/$server; delete it with SERVERPRO_DOGFOOD_KEEP_SERVER=1 SERVERPRO_DOGFOOD_SCENARIOS=delete"
		fail=$((fail + 1))
		skip_case "live create/delete" "kept server state must be removed first"
	elif [[ "$require_clean_start" -eq 1 ]] && ! check_leftovers preflight ""; then
		skip_case "live create/delete" "leftover servers must be removed first"
	elif ! run_live_ok namespace-created "live namespace create" "$bin" namespace create "$namespace"; then
		skip_case "live create/delete" "namespace create failed"
	elif ! write_credentials "$namespace" "$server" "$token" "$SERVERPRO_DOGFOOD_TAILSCALE_TOKEN" "${SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN:-}"; then
		log "FAIL | live write credentials"
		fail=$((fail + 1))
	else
		if [[ "$arm_fallback_delete" -eq 1 ]]; then
			created_namespace="$namespace"
			created_server="$server"
			created_provider="$provider"
		fi
		destructive_started=1
		run_selected_scenarios
		report_server_facts
		expected=""
		[[ "$server_ready" -eq 1 ]] && expected="$server"
		check_leftovers after "$expected"
		leftovers_checked=1
	fi
	unset SERVERPRO_SERVER_PROVIDER_TOKEN SERVERPRO_TAILSCALE_TOKEN SERVERPRO_CLOUDFLARE_TOKEN "$sudo_env_name"
}
