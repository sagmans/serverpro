#!/usr/bin/env bash
# Paid create/delete flow sourced by test-dogfood-live.sh.

DOGFOOD_CREATE_CONFIRMATION="serverpro-live-dogfood"
DOGFOOD_DEFAULT_PROVIDER="hetzner"
DOGFOOD_DEFAULT_SERVER="web"
DOGFOOD_DEFAULT_ADMIN_USER="deploy"
DOGFOOD_DEFAULT_INGRESS="none"

run_destructive_dogfood() {
	if [[ "${SERVERPRO_DOGFOOD_CREATE:-}" != "1" ]]; then
		skip_case "live create/delete" "set SERVERPRO_DOGFOOD_CREATE=1 and SERVERPRO_DOGFOOD_CONFIRM=$DOGFOOD_CREATE_CONFIRMATION"
		return
	fi
	if [[ "${SERVERPRO_DOGFOOD_CONFIRM:-}" != "$DOGFOOD_CREATE_CONFIRMATION" ]]; then
		skip_case "live create/delete" "confirmation token missing"
		return
	fi

	local provider token namespace server admin_user location size image sudo_env_name ingress
	local -a create_args
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

	namespace="${SERVERPRO_DOGFOOD_NAMESPACE:-spdogfood$(date +%s)}"
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

	if ! run_live_ok namespace-created "live namespace create" "$bin" namespace create "$namespace"; then
		skip_case "live create/delete" "namespace create failed"
	elif ! write_credentials "$namespace" "$server" "$token" "$SERVERPRO_DOGFOOD_TAILSCALE_TOKEN" "${SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN:-}"; then
		log "FAIL | live write credentials"
		fail=$((fail + 1))
	else
		# Arm fallback cleanup only when a paid create can start.
		created_namespace="$namespace"
		created_server="$server"
		created_provider="$provider"
		if run_live_ok doctor-report "live server create" "$bin" "${create_args[@]}"; then
			run_live_ok server-status "live server status" "$bin" --non-interactive -n "$namespace" -p "$provider" server status "$server"
			run_live_ok doctor-report "live server doctor" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server"
			run_live_ok bootstrap-complete "live server bootstrap git" "$bin" --non-interactive -n "$namespace" -p "$provider" server bootstrap "$server" git
			if run_live_ok delete-complete "live server delete" "$bin" --non-interactive --yes -n "$namespace" -p "$provider" server delete "$server"; then
				created_namespace=""
				created_server=""
				created_provider=""
			fi
		fi
	fi
	unset SERVERPRO_SERVER_PROVIDER_TOKEN SERVERPRO_TAILSCALE_TOKEN SERVERPRO_CLOUDFLARE_TOKEN "$sudo_env_name"
}
