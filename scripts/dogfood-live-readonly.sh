#!/usr/bin/env bash
# Read-only provider matrix sourced by test-dogfood-live.sh.

# The lib is this flow's only dependency; sourcing it here keeps that explicit
# and lets shellcheck lint the flow alone. Re-sourcing only redefines.
# shellcheck source=scripts/dogfood-live-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/dogfood-live-lib.sh"

# run_readonly_dogfood proves every provider's free API paths with whichever
# tokens are present; a missing token is a visible skip, never a failure.
run_readonly_dogfood() {
	local provider token location
	for provider in $DOGFOOD_PROVIDERS; do
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
