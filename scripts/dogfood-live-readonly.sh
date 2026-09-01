#!/usr/bin/env bash
# Read-only provider matrix sourced by test-dogfood-live.sh.

run_readonly_dogfood() {
	local provider token location
	local -a providers=(hetzner vultr digitalocean)
	for provider in "${providers[@]}"; do
		token="$(provider_token "$provider")"
		if [[ -z "$token" ]]; then
			skip_case "live $provider read-only" "missing SERVERPRO_DOGFOOD_$(env_name_part "$provider")_TOKEN"
			continue
		fi
		export SERVERPRO_SERVER_PROVIDER_TOKEN="$token"
		location="$(provider_location "$provider")"
		run_live_ok diagnostics "live provider doctor $provider" "$bin" --non-interactive provider doctor "$provider"
		run_live_ok catalog "live catalog locations $provider" "$bin" --non-interactive -p "$provider" location list
		run_live_ok catalog "live catalog sizes $provider" "$bin" --non-interactive -p "$provider" size list --location "$location"
		run_live_ok catalog "live catalog images $provider" "$bin" --non-interactive -p "$provider" image list --location "$location"
		run_live_ok list "live discover $provider" "$bin" --non-interactive -p "$provider" server discover
		unset SERVERPRO_SERVER_PROVIDER_TOKEN
	done
}
