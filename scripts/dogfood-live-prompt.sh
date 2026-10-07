#!/usr/bin/env bash
# Interactive input collection sourced by test-dogfood-live.sh.
#
# WHY: live runs need several tokens and a sudo password. Typing them into the
# shell leaves them in history and process environments longer than needed, so
# when a terminal is attached the harness asks for each missing value, masks
# secrets as they are typed, and keeps them in harness variables only. Without
# a terminal (CI, scripts) nothing is asked and the existing skip rules apply.

DOGFOOD_PROMPT_IN_FD=7
DOGFOOD_PROMPT_OUT_FD=8
DOGFOOD_PROMPT_MAX_ATTEMPTS=3
# The CLI refuses shorter sudo passwords, so asking again here saves a paid
# create that would fail later.
DOGFOOD_SUDOPASS_MIN_LENGTH=16

# dogfood_prompt_enabled is true when a person can answer: an interactive
# terminal, or the self-test's scripted input. SERVERPRO_DOGFOOD_NO_PROMPT=1
# always disables prompting.
dogfood_prompt_enabled() {
	[[ "${SERVERPRO_DOGFOOD_NO_PROMPT:-}" != "1" ]] || return 1
	[[ -n "${SERVERPRO_DOGFOOD_TEST_PROMPT_INPUT:-}" ]] && return 0
	[[ -t 0 && -r /dev/tty && -w /dev/tty ]]
}

# open_prompt_streams binds the prompt descriptors. Scripted self-test input
# writes prompts to stderr so the harness log captures them.
open_prompt_streams() {
	if [[ -n "${SERVERPRO_DOGFOOD_TEST_PROMPT_INPUT:-}" ]]; then
		exec 7<"$SERVERPRO_DOGFOOD_TEST_PROMPT_INPUT" 8>&2
	else
		exec 7</dev/tty 8>/dev/tty
	fi
}

close_prompt_streams() {
	exec 7<&- 8>&-
}

# prompt_plain asks for a non-secret value, offering an optional default.
prompt_plain() {
	local var="$1" label="$2" default="${3:-}" value=""
	if [[ -n "$default" ]]; then
		printf '%s [%s]: ' "$label" "$default" >&"$DOGFOOD_PROMPT_OUT_FD"
	else
		printf '%s: ' "$label" >&"$DOGFOOD_PROMPT_OUT_FD"
	fi
	IFS= read -r value <&"$DOGFOOD_PROMPT_IN_FD" || true
	printf -v "$var" '%s' "${value:-$default}"
}

# prompt_secret reads one character at a time, echoing "*" so the operator
# sees progress without the value reaching the screen, scrollback, or logs.
prompt_secret() {
	local var="$1" label="$2" value="" char
	printf '%s: ' "$label" >&"$DOGFOOD_PROMPT_OUT_FD"
	while IFS= read -r -s -n 1 char <&"$DOGFOOD_PROMPT_IN_FD"; do
		case "$char" in
			"" | $'\n' | $'\r') break ;;
			$'\177' | $'\b')
				if [[ -n "$value" ]]; then
					value="${value%?}"
					printf '\b \b' >&"$DOGFOOD_PROMPT_OUT_FD"
				fi
				;;
			*)
				value+="$char"
				printf '*' >&"$DOGFOOD_PROMPT_OUT_FD"
				;;
		esac
	done
	printf '\n' >&"$DOGFOOD_PROMPT_OUT_FD"
	printf -v "$var" '%s' "$value"
}

# prompt_required repeats a prompt until it gets a value, then gives up so an
# unattended terminal cannot loop forever.
prompt_required() {
	local kind="$1" var="$2" label="$3" attempt
	for ((attempt = 1; attempt <= DOGFOOD_PROMPT_MAX_ATTEMPTS; attempt++)); do
		"prompt_$kind" "$var" "$label"
		[[ -n "${!var}" ]] && return 0
		printf 'A value is required.\n' >&"$DOGFOOD_PROMPT_OUT_FD"
	done
	printf 'no value for %s after %s attempts\n' "$var" "$DOGFOOD_PROMPT_MAX_ATTEMPTS" >&2
	exit 2
}

# prompt_sudopass asks twice: the value becomes the test server's sudo
# password, and a typo would lock later keep-mode runs out of the host.
prompt_sudopass() {
	local attempt first second
	for ((attempt = 1; attempt <= DOGFOOD_PROMPT_MAX_ATTEMPTS; attempt++)); do
		prompt_secret first "Sudo password for the test server (min $DOGFOOD_SUDOPASS_MIN_LENGTH characters)"
		# Reject a short value before the repeat prompt so the operator does not
		# retype a password that can never be accepted.
		if ((${#first} < DOGFOOD_SUDOPASS_MIN_LENGTH)); then
			printf 'Too short.\n' >&"$DOGFOOD_PROMPT_OUT_FD"
			continue
		fi
		prompt_secret second "Repeat sudo password"
		if [[ "$first" != "$second" ]]; then
			printf 'Passwords do not match.\n' >&"$DOGFOOD_PROMPT_OUT_FD"
		else
			SERVERPRO_DOGFOOD_SUDOPASS="$first"
			return 0
		fi
	done
	printf 'no valid sudo password after %s attempts\n' "$DOGFOOD_PROMPT_MAX_ATTEMPTS" >&2
	exit 2
}

# provider_token_var names the token variable for the dogfood provider.
provider_token_var() {
	printf 'SERVERPRO_DOGFOOD_%s_TOKEN' "$(env_name_part "$1")"
}

# prompt_missing_dogfood_inputs fills every unset value the selected run needs.
# Values already in the environment are never asked for again.
prompt_missing_dogfood_inputs() {
	dogfood_prompt_enabled || return 0
	open_prompt_streams
	local answer token_var provider_name
	if [[ -z "${SERVERPRO_DOGFOOD_CREATE:-}" ]]; then
		prompt_plain answer "Run paid live server scenarios? This creates billable servers (y/N)"
		[[ "$answer" == y || "$answer" == Y || "$answer" == yes ]] && SERVERPRO_DOGFOOD_CREATE=1
	fi
	provider_name="${SERVERPRO_DOGFOOD_PROVIDER:-$DOGFOOD_DEFAULT_PROVIDER}"
	token_var="$(provider_token_var "$provider_name")"
	if [[ "${SERVERPRO_DOGFOOD_CREATE:-}" == "1" ]]; then
		[[ -n "${!token_var:-}" ]] || prompt_required secret "$token_var" "$provider_name API token"
		[[ -n "${SERVERPRO_DOGFOOD_TAILSCALE_TOKEN:-}" ]] || prompt_required secret SERVERPRO_DOGFOOD_TAILSCALE_TOKEN "Tailscale API token"
		[[ -n "${SERVERPRO_DOGFOOD_TAILNET:-}" ]] || prompt_required plain SERVERPRO_DOGFOOD_TAILNET "Tailnet name (for example example.ts.net)"
		[[ -n "${SERVERPRO_DOGFOOD_SUDOPASS:-}" ]] || prompt_sudopass
		if [[ "${SERVERPRO_DOGFOOD_INGRESS:-}" == cloudflare-tunnel ]]; then
			[[ -n "${SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN:-}" ]] || prompt_required secret SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN "Cloudflare API token"
			[[ -n "${SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID:-}" ]] || prompt_required plain SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID "Cloudflare account ID"
		fi
	elif [[ -z "${!token_var:-}" ]]; then
		# Read-only checks are optional; an empty answer keeps them skipped.
		prompt_secret "$token_var" "$provider_name API token for read-only checks (empty to skip)"
	fi
	close_prompt_streams
}
