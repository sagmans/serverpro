#!/usr/bin/env bash
# WHY pipefail: harness output flows through tee pipelines; a failing producer
# must not be masked by a succeeding consumer when statuses are inspected.
set -uo pipefail

# Live dogfood harness for serverpro. Read-only provider API checks run when
# tokens are available. Create/delete is disabled unless the caller explicitly
# opts into paid, destructive infrastructure work.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bin="${SERVERPRO_BIN:-${1:-./serverpro}}"
validator_script="$script_dir/dogfood_validate.py"
readonly_flow="$script_dir/dogfood-live-readonly.sh"
destructive_flow="$script_dir/dogfood-live-create.sh"
if [[ ! -x "$bin" ]]; then
	printf 'serverpro binary not executable: %s\n' "$bin" >&2
	exit 2
fi
for required_file in "$validator_script" "$readonly_flow" "$destructive_flow"; do
	if [[ ! -r "$required_file" ]]; then
		printf 'live dogfood support file not readable: %s\n' "$required_file" >&2
		exit 2
	fi
done

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/serverpro-live-dogfood.XXXXXX")"
home_dir="$work_dir/home"
out_dir="$work_dir/out"
results="$work_dir/results.txt"
mkdir -p "$home_dir" "$out_dir"

cleanup() {
	if [[ "${SERVERPRO_KEEP_HARNESS_TEMP:-}" == "1" ]]; then
		printf 'kept live dogfood temp dir: %s\n' "$work_dir" >&2
		return
	fi
	rm -rf "$work_dir"
}
trap cleanup EXIT

export HOME="$home_dir"

pass=0
fail=0
skip=0
live=0
created_namespace=""
created_server=""
created_provider=""

log() {
	printf '%s\n' "$*" | tee -a "$results"
}

case_path() {
	printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '_'
}

provider_token() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_TOKEN:-}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_TOKEN:-}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_TOKEN:-}" ;;
		*) return 1 ;;
	esac
}

# Mirrors config.ValidID so operator-supplied identifiers can never traverse
# out of the isolated HOME before the CLI's own validation would run.
valid_dogfood_id() {
	local s="$1"
	[[ -n "$s" && "$s" != "." && "$s" != ".." ]] || return 1
	[[ "$s" != */* && "$s" != *\\* ]] || return 1
	[[ "$s" =~ ^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$ ]] || return 1
}

provider_location() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_LOCATION:-fsn1}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_LOCATION:-ewr}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_LOCATION:-nyc3}" ;;
		*) return 1 ;;
	esac
}

provider_size() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_SIZE:-cx23}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_SIZE:-vc2-1c-1gb}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_SIZE:-s-1vcpu-1gb}" ;;
		*) return 1 ;;
	esac
}

provider_image() {
	case "$1" in
		hetzner) printf '%s' "${SERVERPRO_DOGFOOD_HETZNER_IMAGE:-ubuntu-24.04}" ;;
		vultr) printf '%s' "${SERVERPRO_DOGFOOD_VULTR_IMAGE:-1743}" ;;
		digitalocean) printf '%s' "${SERVERPRO_DOGFOOD_DIGITALOCEAN_IMAGE:-ubuntu-24-04-x64}" ;;
		*) return 1 ;;
	esac
}

validate_case_output() {
	local kind="$1"
	local path="$2"
	local expected_provider="${3:-${provider:-}}"
	local expected_namespace="${4:-${namespace:-}}"
	local expected_server="${5:-${server:-}}"
	python3 "$validator_script" "$kind" "$path" "$expected_provider" "$expected_namespace" "$expected_server"
}

run_case() {
	local expect="$1"
	local validator_kind="$2"
	local label="$3"
	shift 3
	local stem out err status valid
	stem="$(case_path "$label")"
	out="$out_dir/$stem.out"
	err="$out_dir/$stem.err"
	"$@" >"$out" 2>"$err"
	status=$?
	valid=1
	if [[ "$expect" == ok && "$status" -eq 0 ]] && ! validate_case_output "$validator_kind" "$out" >>"$err" 2>&1; then
		valid=0
	fi
	if [[ "$expect" == ok && "$status" -eq 0 && "$valid" -eq 1 ]] || [[ "$expect" == fail && "$status" -ne 0 ]]; then
		log "PASS | $label"
		pass=$((pass + 1))
		return 0
	fi
	log "FAIL | $label | exit $status | output-valid=$valid"
	sed 's/^/  stdout: /' "$out" | tee -a "$results"
	sed 's/^/  stderr: /' "$err" | tee -a "$results"
	fail=$((fail + 1))
	return 1
}

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

skip_case() {
	log "SKIP | $1 | $2"
	skip=$((skip + 1))
}

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

write_credentials() {
	local namespace="$1"
	local server="$2"
	local cred_dir="$HOME/.config/serverpro/namespaces/$namespace/servers/$server"
	mkdir -p "$cred_dir" || return
	chmod 700 "$HOME/.config/serverpro" "$HOME/.config/serverpro/namespaces" "$HOME/.config/serverpro/namespaces/$namespace" "$HOME/.config/serverpro/namespaces/$namespace/servers" "$cred_dir" 2>/dev/null || true
	# WHY env instead of argv: process argument lists are world-readable on
	# shared hosts, so tokens travel through the environment (same channel the
	# CLI itself uses) and only non-secret identifiers stay in argv.
	PROVIDER_TOKEN="$3" TAILSCALE_TOKEN="$4" CLOUDFLARE_TOKEN="$5" \
		python3 -c '
import json
import os
import sys
path, namespace, server = sys.argv[1:4]
payload = {
    "namespace": namespace,
    "server": server,
    "server_provider_token": os.environ["PROVIDER_TOKEN"],
    "tailscale_token": os.environ["TAILSCALE_TOKEN"],
    "tailscale_auth_key": "",
    "cloudflare_token": os.environ["CLOUDFLARE_TOKEN"],
}
with open(path, "w", encoding="utf-8") as fh:
    json.dump(payload, fh, indent=2)
    fh.write("\n")
os.chmod(path, 0o600)
' "$cred_dir/credentials.json" "$namespace" "$server"
}

cleanup_failed=0
cleanup_created_server() {
	if [[ -z "$created_namespace" || -z "$created_server" || -z "$created_provider" ]]; then
		return
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
		return
	fi
	log "CLEANUP-FAIL | delete failed or returned invalid evidence | resources may remain: $created_provider/$created_namespace/$created_server"
	cleanup_failed=1
}
finish() {
	cleanup_created_server
	if [[ "$cleanup_failed" -eq 1 ]]; then
		printf 'preserved live dogfood temp dir after cleanup failure: %s\n' "$work_dir" >&2
		printf 'recover manually, then remove: %s\n' "$work_dir" >&2
		exit 1
	fi
	cleanup
}
trap finish EXIT

# Source only flow ownership; shared execution, credentials, and cleanup remain here.
# shellcheck source=scripts/dogfood-live-readonly.sh
source "$readonly_flow"
# shellcheck source=scripts/dogfood-live-create.sh
source "$destructive_flow"

log "serverpro live dogfood run"
log "binary: $bin"
"$bin" --version | tee -a "$results"
shasum -a 256 "$bin" | tee -a "$results"

run_readonly_dogfood
run_destructive_dogfood

if [[ "${SERVERPRO_REQUIRE_LIVE_DOGFOOD:-}" == "1" && "$live" -eq 0 ]]; then
	log "FAIL | live dogfood requirement | no live API case ran"
	fail=$((fail + 1))
fi

log "SUMMARY | pass=$pass fail=$fail skip=$skip live=$live"
if [[ "$fail" -ne 0 ]]; then
	exit 1
fi
