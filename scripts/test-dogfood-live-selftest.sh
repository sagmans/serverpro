#!/usr/bin/env bash
# Network-free self-test for scripts/test-dogfood-live.sh.
# WHY: the live harness guards paid/destructive infrastructure and handles real
# provider tokens, but its safety logic previously had no executable proof. A
# fake serverpro binary and fake python3 make guard, secret-transport, and
# cleanup behavior testable in CI without tokens or network.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
live_script="$here/test-dogfood-live.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/serverpro-live-selftest.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fakebin="$tmp/fakebin"
mkdir -p "$fakebin"
real_python="$(command -v python3)"

SENT_HETZNER='SENTINEL_HETZNER_SECRET'
SENT_VULTR='SENTINEL_VULTR_SECRET'
SENT_DIGITALOCEAN='SENTINEL_DIGITALOCEAN_SECRET'
SENT_TS='SENTINEL_TAILSCALE_SECRET'
SENT_SUDO='SENTINEL_SUDO_SECRET'
SENT_CF='SENTINEL_CLOUDFLARE_SECRET'
SENT_DECOY='tskey-auth-SENTINEL-DECOY-KEY'

# Fake python3 records argv, then executes the production validator or inline
# credential writer unchanged so serialization assertions cover real code.
cat >"$fakebin/python3" <<'FAKE'
#!/usr/bin/env bash
printf '<%s>\n' "$@" >>"$FAKE_ARGV_DIR/python-argv.log"
exec "$FAKE_REAL_PYTHON" "$@"
FAKE

# Fake serverpro: records argv, honors a delete-status control file so the
# self-test can force cleanup failure, and otherwise succeeds instantly.
cat >"$fakebin/serverpro" <<'FAKE'
#!/usr/bin/env bash
printf '<%s>\n' "$@" >>"$FAKE_ARGV_DIR/serverpro-argv.log"
printf 'CMD' >>"$FAKE_ARGV_DIR/serverpro-command.log"
printf ' <%s>' "$@" >>"$FAKE_ARGV_DIR/serverpro-command.log"
printf '\n' >>"$FAKE_ARGV_DIR/serverpro-command.log"
case " $* " in
	*" --version "*)
		printf 'serverpro version selftest\n'
		exit 0
		;;
esac

# Real summary envelopes keep the shell boundary sensitive to schema regressions.
DOCTOR_SUMMARY_FORMAT='{"namespace":"%s","server":"%s","status":"pass","counts":{"pass":%s,"warn":0,"fail":%s,"skip":0,"total":1},"results":%s,"report_path":"/tmp/selftest-doctor.json"}\n'
DOCTOR_FAILURE_RESULTS='[{"name":"selftest","scope":"fake","status":"fail","evidence":"failed"}]'
args=("$@")
doctor_summary() {
	local server="$1" invalid_semantic="$2"
	local passing=1 failing=0 results='[]'
	if [[ "${FAKE_INVALID_SEMANTIC:-}" == "$invalid_semantic" ]]; then
		# A claimed pass must not hide a failed check from the real validator.
		passing=0 failing=1 results="$DOCTOR_FAILURE_RESULTS"
	fi
	printf "$DOCTOR_SUMMARY_FORMAT" "$namespace" "$server" "$passing" "$failing" "$results"
}
value_after() {
	local flag="$1"
	local index
	for ((index = 0; index + 1 < ${#args[@]}; index++)); do
		if [[ "${args[index]}" == "$flag" ]]; then
			printf '%s' "${args[index + 1]}"
			return
		fi
	done
}
sequence_value() {
	local parent="$1"
	local action="$2"
	local index
	for ((index = 0; index + 2 < ${#args[@]}; index++)); do
		if [[ "${args[index]}" == "$parent" && "${args[index + 1]}" == "$action" ]]; then
			printf '%s' "${args[index + 2]}"
			return
		fi
	done
}
require_provider_token() {
	local command_provider="$1"
	local expected
	case "$command_provider" in
		hetzner) expected="${SENT_HETZNER:-}" ;;
		vultr) expected="${SENT_VULTR:-}" ;;
		digitalocean) expected="${SENT_DIGITALOCEAN:-}" ;;
		*) printf 'unexpected provider: %s\n' "$command_provider" >&2; exit 90 ;;
	esac
	if [[ -z "$expected" || "${SERVERPRO_SERVER_PROVIDER_TOKEN:-}" != "$expected" ]]; then
		printf 'provider token mismatch: %s\n' "$command_provider" >&2
		exit 91
	fi
}
namespace="$(value_after -n)"
provider="$(value_after -p)"
case " $* " in
	*" provider doctor "*) require_provider_token "$(sequence_value provider doctor)" ;;
	*) [[ -z "$provider" ]] || require_provider_token "$provider" ;;
esac

case " $* " in
	*" provider doctor "*)
		case "${FAKE_PROVIDER_DOCTOR_OUTPUT:-valid}" in
			empty) exit 0 ;;
			malformed) printf '{\n'; exit 0 ;;
		esac
		status=pass
		[[ "${FAKE_INVALID_SEMANTIC:-}" == diagnostics-status ]] && status=fail
		printf '[{"status":"%s","message":"selftest"}]\n' "$status"
		;;
	*" location list "*|*" size list "*|*" image list "*)
		invalid_catalog=catalog-locations
		case " $* " in
			*" size list "*) invalid_catalog=catalog-sizes ;;
			*" image list "*) invalid_catalog=catalog-images ;;
		esac
		if [[ "${FAKE_INVALID_SEMANTIC:-}" == "$invalid_catalog" ]]; then
			printf '[{"name":""}]\n'
		else
			printf '[{"name":"selftest"}]\n'
		fi
		;;
	*" server discover "*)
		if [[ "${FAKE_INVALID_SEMANTIC:-}" == inventory-provider ]]; then
			printf '[{"provider":"wrong","id":"123","name":"selftest","namespace":"spdogfood","server":"web","labels_ok":true,"local_state":"missing"}]\n'
		elif [[ -n "${FAKE_LEFTOVER:-}" && -n "$namespace" ]]; then
			printf '[{"provider":"%s","id":"999","name":"%s-old","namespace":"%s","server":"old","labels_ok":true,"local_state":"missing"}]\n' "$provider" "$namespace" "$namespace"
		else
			printf '[]\n'
		fi
		;;
	*" server import "*)
		printf 'HOME %s\n' "$HOME" >>"$FAKE_ARGV_DIR/import-home.log"
		status=imported
		[[ "${FAKE_INVALID_SEMANTIC:-}" == import-status ]] && status=failed
		printf '[{"namespace":"%s","server":"%s","provider":"%s","provider_id":"123","status":"%s","config_path":"c","state_path":"s"}]\n' "$namespace" "$(sequence_value server import)" "$provider" "$status"
		;;
	*" server stop "*|*" server start "*|*" server restart "*)
		case " $* " in
			*" server stop "*) printf 'off' >"$FAKE_ARGV_DIR/power"; server="$(sequence_value server stop)" ;;
			*" server start "*) printf 'on' >"$FAKE_ARGV_DIR/power"; server="$(sequence_value server start)" ;;
			*" server restart "*)
				server="$(sequence_value server restart)"
				boots=0
				[[ -f "$FAKE_ARGV_DIR/boot" ]] && boots="$(cat "$FAKE_ARGV_DIR/boot")"
				printf '%s' "$((boots + 1))" >"$FAKE_ARGV_DIR/boot"
				;;
		esac
		printf '{"namespace":"%s","server":"%s","provider":"%s","power":"%s"}\n' "$namespace" "$server" "$provider" "$(cat "$FAKE_ARGV_DIR/power" 2>/dev/null || printf on)"
		;;
	*" namespace create "*)
		namespace="$(sequence_value namespace create)"
		status=created
		[[ "${FAKE_INVALID_SEMANTIC:-}" == namespace-status ]] && status=planned
		printf '{"status":"%s","namespace":"%s"}\n' "$status" "$namespace"
		;;
	*" server create "*)
		if [[ "${SERVERPRO_CLOUDFLARE_TOKEN:-}" == "${SENT_CF:-}" ]]; then
			printf 'exact\n' >"$FAKE_ARGV_DIR/cloudflare-env"
		fi
		server="$(sequence_value server create)"
		if [[ -n "${FAKE_INTERRUPT_ON_CREATE:-}" ]]; then
			# The harness must still delete the throwaway server after a signal.
			kill -TERM "$PPID"
			exit 1
		fi
		# A create rerun keeps the recorded node unless a test asks for a swap.
		state_file="$HOME/.local/state/serverpro/namespaces/$namespace/servers/$server.json"
		node=node-recorded
		if [[ -f "$state_file" && -n "${FAKE_NODE_SWAP_ON_RERUN:-}" ]]; then
			node=node-swapped
		fi
		mkdir -p "$(dirname "$state_file")"
		printf '{"created_at":"%s","compute":{"id":"srv-1","name":"%s-%s"},"tailscale":{"name":"%s-%s.selftest.ts.net","node_id":"%s"}}\n' \
			"${FAKE_CREATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" "$namespace" "$server" "$namespace" "$server" "$node" >"$state_file"
		doctor_summary "$server" create-doctor-status
		;;
	*" server status "*)
		server="$(sequence_value server status)"
		power=running
		[[ -f "$FAKE_ARGV_DIR/power" ]] && power="$(cat "$FAKE_ARGV_DIR/power")"
		case "${FAKE_INVALID_SEMANTIC:-}" in
			status-provider) provider=wrong ;;
			status-power) power= ;;
		esac
		printf '{"namespace":"%s","server":"%s","provider":"%s","power":"%s"}\n' "$namespace" "$server" "$provider" "$power"
		;;
	*" server doctor "*)
		doctor_summary "$(sequence_value server doctor)" server-doctor-status
		;;
	*" server bootstrap "*)
		server="$(sequence_value server bootstrap)"
		action=bootstrap
		[[ "${FAKE_INVALID_SEMANTIC:-}" == bootstrap-action ]] && action=planned
		printf '{"status":"complete","action":"%s","namespace":"%s","server":"%s","target":"git"}\n' "$action" "$namespace" "$server"
		;;
	*" server delete "*)
		st=0
		[[ -f "$FAKE_ARGV_DIR/delete-status" ]] && st="$(cat "$FAKE_ARGV_DIR/delete-status")"
		if [[ "$st" -eq 0 ]]; then
			case "${FAKE_DELETE_OUTPUT:-valid}" in
				empty) exit 0 ;;
				malformed) printf '{\n'; exit 0 ;;
			esac
			server="$(sequence_value server delete)"
			# A second signal during teardown must not abort the delete.
			[[ -z "${FAKE_SIGNAL_DURING_DELETE:-}" ]] || kill -INT "$PPID"
			rm -f "$HOME/.local/state/serverpro/namespaces/$namespace/servers/$server.json"
			action=delete
			case "${FAKE_INVALID_SEMANTIC:-}" in
				delete-provider) provider=wrong ;;
			esac
			printf '{"status":"complete","action":"%s","namespace":"%s","server":"%s","provider":"%s"}\n' "$action" "$namespace" "$server" "$provider"
		fi
		exit "$st"
		;;
	*)
		printf '{}\n'
		;;
esac
FAKE

# Fake tailscale: records argv and stdin separately so tests prove the sudo
# password and decoy key travel only on stdin. Boot IDs follow the restart
# counter the fake serverpro keeps, so reboot detection is exercised.
cat >"$fakebin/tailscale" <<'FAKE'
#!/usr/bin/env bash
printf '<%s>\n' "$@" >>"$FAKE_ARGV_DIR/tailscale-argv.log"
cat >>"$FAKE_ARGV_DIR/tailscale-stdin.log"
case "$*" in
	*boot_id*)
		# A hung host must be cut off by the harness's own SSH timeout.
		[[ -z "${FAKE_SSH_HANG:-}" ]] || sleep 20
		printf 'boot-%s\n' "$(cat "$FAKE_ARGV_DIR/boot" 2>/dev/null || printf 0)"
		;;
	*sudo*)
		# Without a TTY the cached sudo credential only holds when both sudo
		# calls share a parent shell, so exec-ing a second sudo would fail live.
		if [[ "$*" == *"exec sudo"* || "$*" != *"sudo -n sh -s"* ]]; then
			printf 'unsupported sudo shape\n' >&2
			exit 97
		fi
		[[ -z "${FAKE_DECOY_JOIN_FAIL:-}" ]] || exit 1
		;;
esac
FAKE

# Fake curl: records argv and the stdin config, then answers the three
# Tailscale API calls the identity scenario makes.
cat >"$fakebin/curl" <<'FAKE'
#!/usr/bin/env bash
printf '<%s>\n' "$@" >>"$FAKE_ARGV_DIR/curl-argv.log"
cat >>"$FAKE_ARGV_DIR/curl-stdin.log"
method=GET
args=("$@")
for ((i = 0; i + 1 < ${#args[@]}; i++)); do
	[[ "${args[i]}" == -X ]] && method="${args[i + 1]}"
done
url="${args[${#args[@]} - 1]}"
case "$method $url" in
	"POST "*/keys) printf '{"id":"kdecoy","key":"%s"}\n' "$SENT_DECOY" ;;
	"GET "*/devices)
		device='{"hostname":"spdogfooda-web","tags":["tag:serverpro-spdogfooda"],"connectedToControl":true}'
		if [[ -n "${FAKE_DECOY_MISSING:-}" ]]; then
			printf '{"devices":[%s]}\n' "$device"
		else
			printf '{"devices":[%s,%s]}\n' "$device" "$device"
		fi
		;;
	"DELETE "*) printf '{}\n' ;;
	*) exit 22 ;;
esac
FAKE

chmod +x "$fakebin/python3" "$fakebin/serverpro" "$fakebin/tailscale" "$fakebin/curl"

fails=0
note() { printf '%s\n' "$*"; }
ok() { note "PASS | $1"; }
bad() { note "FAIL | $1"; fails=$((fails + 1)); }
check() { # check <label> <command...>
	local label="$1"
	shift
	if "$@" >/dev/null 2>&1; then ok "$label"; else bad "$label"; fi
}
check_absent() { # check_absent <label> <needle> <path>
	if [[ -e "$3" ]] && grep -Fq "$2" "$3"; then bad "$1"; else ok "$1"; fi
}
check_command() { # check_command <label> <exact recorded command>
	check "$1" grep -Fqx "$2" "$FAKE_ARGV_DIR/serverpro-command.log"
}
check_file_bytes() { # check_file_bytes <label> <expected bytes> <path>
	if [[ -f "$3" ]] && cmp -s "$3" <(printf '%s' "$2"); then
		ok "$1"
	else
		bad "$1"
	fi
}
check_credentials_document() { # check_credentials_document <path>
	CREDENTIALS_PATH="$1" EXPECTED_PROVIDER="$SENT_HETZNER" \
		EXPECTED_TAILSCALE="$SENT_TS" EXPECTED_CLOUDFLARE="$SENT_CF" \
		"$real_python" - <<'PY'
import json
import os

def unique_object(pairs):
    keys = [key for key, _ in pairs]
    if len(keys) != len(set(keys)):
        raise ValueError("duplicate credential field")
    return dict(pairs)


with open(os.environ["CREDENTIALS_PATH"], encoding="utf-8") as stream:
    actual = json.load(stream, object_pairs_hook=unique_object)
expected = {
    "namespace": "spdogfooda",
    "server": "web",
    "server_provider_token": os.environ["EXPECTED_PROVIDER"],
    "tailscale_token": os.environ["EXPECTED_TAILSCALE"],
    "tailscale_auth_key": "",
    "cloudflare_token": os.environ["EXPECTED_CLOUDFLARE"],
}
if actual != expected:
    raise SystemExit("credential document mismatch")
PY
}
check_no_create_or_credentials() { # check_no_create_or_credentials <label>
	local label="$1"
	if [[ -e "$FAKE_ARGV_DIR/serverpro-argv.log" ]] && grep -Fqx '<create>' "$FAKE_ARGV_DIR/serverpro-argv.log"; then
		bad "$label no mutating create command"
	else
		ok "$label no mutating create command"
	fi
	if [[ -n "$(find "$scenario_tmp" -name credentials.json -print -quit)" ]]; then
		bad "$label no credentials written"
	else
		ok "$label no credentials written"
	fi
}

# run_harness <scenario> <delete status> <extra env assignments...>
# Runs the live script with an isolated TMPDIR and fresh fake-argv logs.
run_harness() {
	local scenario="$1"
	local delete_status="$2"
	shift 2
	local stmp="$tmp/$scenario"
	export FAKE_ARGV_DIR="$stmp/argv"
	# Operators always have a home; keep-mode alias checks resolve it physically.
	mkdir -p "$FAKE_ARGV_DIR" "$stmp/htmp" "$stmp/home"
	[[ -z "$delete_status" ]] || printf '%s\n' "$delete_status" >"$FAKE_ARGV_DIR/delete-status"
	env -i PATH="$fakebin:/usr/bin:/bin" HOME="$stmp/home" \
		TMPDIR="$stmp/htmp" FAKE_ARGV_DIR="$FAKE_ARGV_DIR" FAKE_REAL_PYTHON="$real_python" \
		SERVERPRO_BIN="$fakebin/serverpro" \
		SENT_HETZNER="$SENT_HETZNER" SENT_VULTR="$SENT_VULTR" \
		SENT_DIGITALOCEAN="$SENT_DIGITALOCEAN" SENT_CF="$SENT_CF" SENT_DECOY="$SENT_DECOY" \
		"$@" \
		bash "$live_script" >"$stmp/harness.log" 2>&1
	harness_rc=$?
	cp "$stmp/harness.log" "$stmp/htmp/"
	scenario_tmp="$stmp"
}

work_dir() {
	find "$scenario_tmp/htmp" -maxdepth 1 -type d -name 'serverpro-live-dogfood.*' | head -1
}

create_prerequisites=(
	"SERVERPRO_DOGFOOD_PROVIDER=hetzner"
	"SERVERPRO_DOGFOOD_HETZNER_TOKEN=$SENT_HETZNER"
	"SERVERPRO_DOGFOOD_TAILSCALE_TOKEN=$SENT_TS"
	"SERVERPRO_DOGFOOD_TAILNET=selftest-tailnet"
	"SERVERPRO_DOGFOOD_SUDOPASS=$SENT_SUDO"
	"SERVERPRO_DOGFOOD_NAMESPACE=spdogfooda"
	"SERVERPRO_DOGFOOD_SERVER=web"
)
create_env=(
	"SERVERPRO_DOGFOOD_CREATE=1"
	"SERVERPRO_DOGFOOD_CONFIRM=serverpro-live-dogfood"
	"${create_prerequisites[@]}"
)

note "scenario A: all-provider happy path keeps every secret private"
run_harness scenarioA "" env "${create_env[@]}" \
	SERVERPRO_DOGFOOD_VULTR_TOKEN="$SENT_VULTR" \
	SERVERPRO_DOGFOOD_DIGITALOCEAN_TOKEN="$SENT_DIGITALOCEAN" \
	SERVERPRO_DOGFOOD_INGRESS=cloudflare-tunnel \
	SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN="$SENT_CF" \
	SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID=selftest-account \
	SERVERPRO_KEEP_HARNESS_TEMP=1
wd="$(work_dir)"
if [[ "$harness_rc" -eq 0 ]]; then
	ok "A exit zero"
else
	bad "A exit zero"
	sed 's/^/  log: /' "$scenario_tmp/harness.log"
fi
for secret in "$SENT_HETZNER" "$SENT_VULTR" "$SENT_DIGITALOCEAN"; do
	check_absent "A provider secret not in python argv" "$secret" "$FAKE_ARGV_DIR/python-argv.log"
done
check_absent "A tailscale secret not in python argv" "$SENT_TS" "$FAKE_ARGV_DIR/python-argv.log"
check_absent "A Cloudflare secret not in python argv" "$SENT_CF" "$FAKE_ARGV_DIR/python-argv.log"
for secret in "$SENT_HETZNER" "$SENT_VULTR" "$SENT_DIGITALOCEAN"; do
	check_absent "A provider secret not in serverpro argv" "$secret" "$FAKE_ARGV_DIR/serverpro-argv.log"
done
check_absent "A sudo secret not in serverpro argv" "$SENT_SUDO" "$FAKE_ARGV_DIR/serverpro-argv.log"
check_absent "A Cloudflare secret not in serverpro argv" "$SENT_CF" "$FAKE_ARGV_DIR/serverpro-argv.log"
check "A exact Cloudflare secret reached serverpro environment" grep -Fqx exact "$FAKE_ARGV_DIR/cloudflare-env"
for provider in hetzner vultr digitalocean; do
	case "$provider" in
		hetzner) location=fsn1 ;;
		vultr) location=ewr ;;
		digitalocean) location=nyc3 ;;
	esac
	check "A $provider provider doctor passed" grep -Fq "PASS | live provider doctor $provider" "$scenario_tmp/harness.log"
	check "A $provider locations passed" grep -Fq "PASS | live catalog locations $provider" "$scenario_tmp/harness.log"
	check "A $provider sizes passed" grep -Fq "PASS | live catalog sizes $provider" "$scenario_tmp/harness.log"
	check "A $provider images passed" grep -Fq "PASS | live catalog images $provider" "$scenario_tmp/harness.log"
	check "A $provider discover passed" grep -Fq "PASS | live discover $provider" "$scenario_tmp/harness.log"
	check_command "A $provider doctor argv" "CMD <--non-interactive> <provider> <doctor> <$provider>"
	check_command "A $provider locations argv" "CMD <--non-interactive> <-p> <$provider> <location> <list>"
	check_command "A $provider sizes argv" "CMD <--non-interactive> <-p> <$provider> <size> <list> <--location> <$location>"
	check_command "A $provider images argv" "CMD <--non-interactive> <-p> <$provider> <image> <list> <--location> <$location>"
	check_command "A $provider discover argv" "CMD <--non-interactive> <-p> <$provider> <server> <discover>"
done
check_command "A live server doctor argv" "CMD <--non-interactive> <-n> <spdogfooda> <-p> <hetzner> <server> <doctor> <web>"
if [[ -n "$wd" ]]; then
	leaked=0
	while IFS= read -r f; do
		if grep -Fq "$SENT_HETZNER" "$f" || grep -Fq "$SENT_VULTR" "$f" || grep -Fq "$SENT_DIGITALOCEAN" "$f" || grep -Fq "$SENT_TS" "$f" || grep -Fq "$SENT_SUDO" "$f" || grep -Fq "$SENT_CF" "$f"; then
			note "  leak in $f"
			leaked=1
		fi
	done < <(find "$wd/out" -type f; printf '%s\n' "$wd/results.txt")
	if [[ "$leaked" -eq 0 ]]; then
		ok "A no secrets in results/out artifacts"
	else
		bad "A no secrets in results/out artifacts"
	fi
	creds="$wd/home/.config/serverpro/namespaces/spdogfooda/servers/web/credentials.json"
	check "A exact credential document written via env transport" check_credentials_document "$creds"
	duplicate_creds="$scenario_tmp/duplicate-credentials.json"
	printf '{"namespace":"wrong","namespace":"spdogfooda","server":"web","server_provider_token":"%s","tailscale_token":"%s","tailscale_auth_key":"","cloudflare_token":"%s"}\n' \
		"$SENT_HETZNER" "$SENT_TS" "$SENT_CF" >"$duplicate_creds"
	if check_credentials_document "$duplicate_creds" >/dev/null 2>&1; then
		bad "A duplicate credential field rejected"
	else
		ok "A duplicate credential field rejected"
	fi
	perm="$(stat -c '%a' "$creds" 2>/dev/null || stat -f '%Lp' "$creds")"
	if [[ "$perm" == "600" ]]; then
		ok "A credentials mode 0600"
	else
		bad "A credentials mode 0600 ($perm)"
	fi
else
	bad "A work dir preserved for inspection"
fi

note "scenario N: default namespace stays stable across runs"
# A per-run namespace would add new tailnet policy entries on every run.
default_namespace_env=()
for assignment in "${create_env[@]}"; do
	[[ "$assignment" == SERVERPRO_DOGFOOD_NAMESPACE=* ]] || default_namespace_env+=("$assignment")
done
for run in 1 2; do
	run_harness "scenarioN-$run" "" env "${default_namespace_env[@]}" SERVERPRO_DOGFOOD_INGRESS=none
	if [[ "$harness_rc" -eq 0 ]]; then ok "N run $run exit zero"; else bad "N run $run exit zero"; fi
	check_command "N run $run uses fixed namespace" "CMD <namespace> <create> <spdogfood>"
done

note "guard scenarios: destructive flow needs every explicit opt-in"
for guard in no-opt-in wrong-create missing-confirmation wrong-confirmation missing-provider-token missing-tailscale-token missing-tailnet missing-sudopass; do
	case "$guard" in
		no-opt-in)
			run_harness "guard-$guard" "" env "${create_prerequisites[@]}" \
				SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		wrong-create)
			run_harness "guard-$guard" "" env "${create_prerequisites[@]}" \
				SERVERPRO_DOGFOOD_CREATE=yes SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		missing-confirmation)
			run_harness "guard-$guard" "" env "${create_prerequisites[@]}" \
				SERVERPRO_DOGFOOD_CREATE=1 SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		wrong-confirmation)
			run_harness "guard-$guard" "" env "${create_prerequisites[@]}" \
				SERVERPRO_DOGFOOD_CREATE=1 SERVERPRO_DOGFOOD_CONFIRM=wrong \
				SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		missing-provider-token)
			run_harness "guard-$guard" "" env "${create_env[@]}" \
				SERVERPRO_DOGFOOD_HETZNER_TOKEN= SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		missing-tailscale-token)
			run_harness "guard-$guard" "" env "${create_env[@]}" \
				SERVERPRO_DOGFOOD_TAILSCALE_TOKEN= SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		missing-tailnet)
			run_harness "guard-$guard" "" env "${create_env[@]}" \
				SERVERPRO_DOGFOOD_TAILNET= SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		missing-sudopass)
			run_harness "guard-$guard" "" env "${create_env[@]}" \
				SERVERPRO_DOGFOOD_SUDOPASS= SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
	esac
	if [[ "$harness_rc" -eq 0 ]]; then ok "guard $guard exit zero"; else bad "guard $guard exit zero"; fi
	check "guard $guard skipped destructive flow" grep -Fq "SKIP | live create/delete" "$scenario_tmp/harness.log"
	check_no_create_or_credentials "guard $guard"
done

for guard in missing-cloudflare-token missing-cloudflare-account; do
	case "$guard" in
		missing-cloudflare-token)
			run_harness "guard-$guard" "" env "${create_env[@]}" \
				SERVERPRO_DOGFOOD_INGRESS=cloudflare-tunnel \
				SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID=selftest-account \
				SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
		missing-cloudflare-account)
			run_harness "guard-$guard" "" env "${create_env[@]}" \
				SERVERPRO_DOGFOOD_INGRESS=cloudflare-tunnel \
				SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN="$SENT_CF" \
				SERVERPRO_KEEP_HARNESS_TEMP=1
			;;
	esac
	if [[ "$harness_rc" -ne 0 ]]; then ok "guard $guard nonzero exit"; else bad "guard $guard nonzero exit"; fi
	check "guard $guard reported missing prerequisite" grep -Fq "requires SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN and SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID" "$scenario_tmp/harness.log"
	check_no_create_or_credentials "guard $guard"
done

note "scenario B: invalid namespace aborts before any write"
run_harness scenarioB "" env "${create_env[@]}" SERVERPRO_DOGFOOD_INGRESS=none SERVERPRO_DOGFOOD_NAMESPACE=../evil
if [[ "$harness_rc" -ne 0 ]]; then ok "B nonzero exit"; else bad "B nonzero exit"; fi
check "B harness reports invalid namespace" grep -Fqi "invalid" "$scenario_tmp/harness.log"
if grep -Fq "evil" "$FAKE_ARGV_DIR/serverpro-argv.log" 2>/dev/null; then bad "B serverpro never sees traversal namespace"; else ok "B serverpro never sees traversal namespace"; fi
if [[ -n "$(find "$scenario_tmp" -name 'evil' -print -quit)" ]]; then bad "B no escaped directory created"; else ok "B no escaped directory created"; fi

note "scenario C: unknown ingress aborts before create"
run_harness scenarioC "" env "${create_env[@]}" SERVERPRO_DOGFOOD_INGRESS=bogus
if [[ "$harness_rc" -ne 0 ]]; then ok "C nonzero exit"; else bad "C nonzero exit"; fi
check "C harness reports invalid ingress" grep -Fq "SERVERPRO_DOGFOOD_INGRESS" "$scenario_tmp/harness.log"
if grep -Fq "create" "$FAKE_ARGV_DIR/serverpro-argv.log" 2>/dev/null; then bad "C create never attempted"; else ok "C create never attempted"; fi

note "scenario D: failed delete retains markers and artifacts"
run_harness scenarioD 1 env "${create_env[@]}" SERVERPRO_DOGFOOD_INGRESS=none
wd="$(work_dir)"
if [[ "$harness_rc" -ne 0 ]]; then ok "D nonzero exit on delete failure"; else bad "D nonzero exit on delete failure"; fi
if [[ -n "$wd" && -f "$wd/results.txt" ]]; then ok "D artifacts preserved after cleanup failure"; else bad "D artifacts preserved after cleanup failure"; fi
if [[ -n "$wd" ]]; then
	check "D delete failure recorded" grep -Fq "FAIL | live server delete" "$wd/results.txt"
	check "D cleanup retry attempted (markers retained)" grep -Eq "CLEANUP" "$wd/results.txt"
fi

note "scenario E: empty successful output fails semantic validation"
run_harness scenarioE "" env SERVERPRO_DOGFOOD_HETZNER_TOKEN="$SENT_HETZNER" SERVERPRO_REQUIRE_LIVE_DOGFOOD=1 FAKE_PROVIDER_DOCTOR_OUTPUT=empty
if [[ "$harness_rc" -ne 0 ]]; then ok "E nonzero exit"; else bad "E nonzero exit"; fi
check "E output validation failure recorded" grep -Fq "FAIL | live provider doctor hetzner" "$scenario_tmp/harness.log"

note "scenario F: malformed successful output fails semantic validation"
run_harness scenarioF "" env SERVERPRO_DOGFOOD_HETZNER_TOKEN="$SENT_HETZNER" SERVERPRO_REQUIRE_LIVE_DOGFOOD=1 FAKE_PROVIDER_DOCTOR_OUTPUT=malformed
if [[ "$harness_rc" -ne 0 ]]; then ok "F nonzero exit"; else bad "F nonzero exit"; fi
check "F output validation failure recorded" grep -Fq "FAIL | live provider doctor hetzner" "$scenario_tmp/harness.log"

note "scenario G: empty fallback-delete output retains recovery evidence"
run_harness scenarioG "" env "${create_env[@]}" SERVERPRO_DOGFOOD_INGRESS=none FAKE_DELETE_OUTPUT=empty
wd="$(work_dir)"
if [[ "$harness_rc" -ne 0 ]]; then ok "G nonzero exit"; else bad "G nonzero exit"; fi
if [[ -n "$wd" && -f "$wd/results.txt" ]]; then ok "G artifacts preserved"; else bad "G artifacts preserved"; fi
if [[ -n "$wd" ]]; then
	check "G fallback output validation failure recorded" grep -Fq "CLEANUP-FAIL" "$wd/results.txt"
	check_file_bytes "G exact empty cleanup output retained" "" "$wd/out/cleanup-delete.out"
	check_file_bytes "G exact cleanup validator detail retained" \
		$'invalid JSON output: Expecting value: line 1 column 1 (char 0)\n' \
		"$wd/out/cleanup-delete.err"
	printf 'invalid JSON output: Expecting value: line 1 column 1 (char 0)\n\n' >"$scenario_tmp/extra-newline"
	if cmp -s "$scenario_tmp/extra-newline" <(printf '%s' $'invalid JSON output: Expecting value: line 1 column 1 (char 0)\n'); then
		bad "G byte comparison rejects added newline"
	else
		ok "G byte comparison rejects added newline"
	fi
fi

note "scenario H: command wiring rejects invalid semantics"
invalid_semantics=(
	"diagnostics-status|live provider doctor hetzner|readonly"
	"catalog-locations|live catalog locations hetzner|readonly"
	"catalog-sizes|live catalog sizes hetzner|readonly"
	"catalog-images|live catalog images hetzner|readonly"
	"inventory-provider|live discover hetzner|readonly"
	"namespace-status|live namespace create|create"
	"create-doctor-status|live server create|create"
	"status-power|live server status|create"
	"server-doctor-status|live server doctor|create"
	"bootstrap-action|live server bootstrap git|create"
	"delete-provider|live server delete|create"
)
for entry in "${invalid_semantics[@]}"; do
	IFS='|' read -r semantic label flow <<<"$entry"
	if [[ "$flow" == readonly ]]; then
		run_harness "scenarioH-$semantic" "" env \
			SERVERPRO_DOGFOOD_HETZNER_TOKEN="$SENT_HETZNER" \
			SERVERPRO_REQUIRE_LIVE_DOGFOOD=1 \
			FAKE_INVALID_SEMANTIC="$semantic"
	else
		run_harness "scenarioH-$semantic" "" env "${create_env[@]}" \
			SERVERPRO_DOGFOOD_INGRESS=none FAKE_INVALID_SEMANTIC="$semantic"
	fi
	if [[ "$harness_rc" -ne 0 ]]; then ok "H $semantic rejected"; else bad "H $semantic rejected"; fi
	check "H $semantic failure recorded" grep -Fq "FAIL | $label" "$scenario_tmp/harness.log"
	if [[ "$semantic" == delete-* ]]; then
		wd="$(work_dir)"
		if [[ -n "$wd" && -f "$wd/results.txt" ]]; then
			check "H $semantic cleanup evidence retained" grep -Fq "CLEANUP-FAIL" "$wd/results.txt"
			check_file_bytes "H $semantic exact cleanup payload retained" \
				$'{"status":"complete","action":"delete","namespace":"spdogfooda","server":"web","provider":"wrong"}\n' \
				"$wd/out/cleanup-delete.out"
			check_file_bytes "H $semantic exact validator detail retained" \
				$'provider mismatch\n' "$wd/out/cleanup-delete.err"
		else
			bad "H $semantic cleanup evidence retained"
		fi
	fi
done

fast_waits=(SERVERPRO_DOGFOOD_RECOVERY_TIMEOUT=3 SERVERPRO_DOGFOOD_POLL_INTERVAL=1)
all_scenarios="create,status,doctor,fix,bootstrap,power,import,identity,delete"

check_no_secret_in() { # check_no_secret_in <label> <path...>
	local label="$1" path secret
	shift
	for path in "$@"; do
		# A renamed or missing log must fail rather than pass unchecked.
		if [[ ! -e "$path" ]]; then
			bad "$label (missing $path)"
			return
		fi
		for secret in "$SENT_HETZNER" "$SENT_TS" "$SENT_SUDO" "$SENT_DECOY"; do
			if grep -rFq "$secret" "$path"; then
				bad "$label ($path)"
				return
			fi
		done
	done
	ok "$label"
}

note "scenario S: scenario selection fails closed"
run_harness scenarioS-unknown "" env "${create_env[@]}" SERVERPRO_DOGFOOD_SCENARIOS=create,reboot
if [[ "$harness_rc" -eq 2 ]]; then ok "S unknown scenario exit 2"; else bad "S unknown scenario exit 2 ($harness_rc)"; fi
check_no_create_or_credentials "S unknown scenario"
run_harness scenarioS-nocreate "" env "${create_env[@]}" SERVERPRO_DOGFOOD_SCENARIOS=doctor
if [[ "$harness_rc" -eq 2 ]]; then ok "S server scenario without create exit 2"; else bad "S server scenario without create exit 2 ($harness_rc)"; fi
check_no_create_or_credentials "S server scenario without create"

note "scenario P: every scenario runs in order and keeps secrets off argv"
run_harness scenarioP "" env "${create_env[@]}" "${fast_waits[@]}" \
	SERVERPRO_DOGFOOD_SCENARIOS="$all_scenarios" SERVERPRO_KEEP_HARNESS_TEMP=1
wd="$(work_dir)"
if [[ "$harness_rc" -eq 0 ]]; then
	ok "P exit zero"
else
	bad "P exit zero"
	sed 's/^/  log: /' "$scenario_tmp/harness.log"
fi
scope="<-n> <spdogfooda> <-p> <hetzner>"
check_command "P stop argv" "CMD <--non-interactive> <--yes> $scope <server> <stop> <web>"
check_command "P start argv" "CMD <--non-interactive> <--yes> $scope <server> <start> <web>"
check_command "P restart argv" "CMD <--non-interactive> <--yes> $scope <server> <restart> <web>"
check_command "P fix argv" "CMD <--non-interactive> $scope <server> <doctor> <web> <--fix>"
check "P reboot proven by boot id" grep -Fq "PASS | live server rebooted" "$scenario_tmp/harness.log"
check "P doctor after restart" grep -Fq "PASS | live server doctor after restart" "$scenario_tmp/harness.log"
check "P import ran in a separate HOME" grep -Fq "HOME $wd/import-home" "$FAKE_ARGV_DIR/import-home.log"
check "P doctor after import" grep -Fq "PASS | live server doctor after import" "$scenario_tmp/harness.log"
check "P decoy enrolled" grep -Fq "PASS | live identity decoy enrolled" "$scenario_tmp/harness.log"
check "P recorded node unchanged" grep -Fq "PASS | live recorded node unchanged beside identity decoy" "$scenario_tmp/harness.log"
check "P decoy removed" grep -Fq "CLEANUP | identity decoy removed" "$scenario_tmp/harness.log"
check "P decoy key revoked" grep -Fq "keys/kdecoy>" "$FAKE_ARGV_DIR/curl-argv.log"
check "P token sent via curl stdin" grep -Fq "Authorization: Bearer $SENT_TS" "$FAKE_ARGV_DIR/curl-stdin.log"
check "P decoy key sent via ssh stdin" grep -Fq "$SENT_DECOY" "$FAKE_ARGV_DIR/tailscale-stdin.log"
check "P sudo password sent via ssh stdin" grep -Fq "$SENT_SUDO" "$FAKE_ARGV_DIR/tailscale-stdin.log"
check_no_secret_in "P no secret in any argv" "$FAKE_ARGV_DIR/serverpro-argv.log" "$FAKE_ARGV_DIR/python-argv.log" "$FAKE_ARGV_DIR/tailscale-argv.log" "$FAKE_ARGV_DIR/curl-argv.log"
[[ -n "$wd" ]] && check_no_secret_in "P no secret in artifacts" "$wd/out" "$wd/results.txt"
for scenario in ${all_scenarios//,/ }; do
	check "P $scenario summary pass" grep -Fq "SCENARIO | $scenario | pass |" "$scenario_tmp/harness.log"
done
check "P server facts reported" grep -Fq "SERVER | hetzner/spdogfooda/web" "$scenario_tmp/harness.log"
check "P leftover check after run" grep -Fq "PASS | live leftover check after" "$scenario_tmp/harness.log"

note "scenario K: keep mode reuses one server across runs"
keep_home="$tmp/keep-home"
keep_env=("${create_env[@]}" "${fast_waits[@]}" SERVERPRO_DOGFOOD_KEEP_SERVER=1 SERVERPRO_DOGFOOD_HOME="$keep_home")
run_harness scenarioK-create "" env "${keep_env[@]}" SERVERPRO_DOGFOOD_SCENARIOS=create,doctor
if [[ "$harness_rc" -eq 0 ]]; then ok "K create exit zero"; else bad "K create exit zero"; sed 's/^/  log: /' "$scenario_tmp/harness.log"; fi
check_absent "K create run never deletes" "<delete>" "$FAKE_ARGV_DIR/serverpro-argv.log"
check "K state persisted" test -f "$keep_home/.local/state/serverpro/namespaces/spdogfooda/servers/web.json"
perm="$(stat -c '%a' "$keep_home" 2>/dev/null || stat -f '%Lp' "$keep_home")"
if [[ "$perm" == "700" ]]; then ok "K home mode 0700"; else bad "K home mode 0700 ($perm)"; fi
run_harness scenarioK-reuse "" env "${keep_env[@]}" SERVERPRO_DOGFOOD_SCENARIOS=doctor,fix
if [[ "$harness_rc" -eq 0 ]]; then ok "K reuse exit zero"; else bad "K reuse exit zero"; sed 's/^/  log: /' "$scenario_tmp/harness.log"; fi
check_absent "K reuse never creates" "<server> <create>" "$FAKE_ARGV_DIR/serverpro-command.log"
check "K reuse ran doctor" grep -Fq "PASS | live server doctor after fix" "$scenario_tmp/harness.log"
check "K kept server reported" grep -Fq "kept=yes" "$scenario_tmp/harness.log"
run_harness scenarioK-delete "" env "${keep_env[@]}" SERVERPRO_DOGFOOD_SCENARIOS=delete
check "K delete scenario removes server" grep -Fq "PASS | live server delete" "$scenario_tmp/harness.log"
check "K state removed" test ! -f "$keep_home/.local/state/serverpro/namespaces/spdogfooda/servers/web.json"
run_harness scenarioK-badhome "" env "${create_env[@]}" SERVERPRO_DOGFOOD_KEEP_SERVER=1 SERVERPRO_DOGFOOD_HOME=relative/home
if [[ "$harness_rc" -eq 2 ]]; then ok "K relative home rejected"; else bad "K relative home rejected ($harness_rc)"; fi

note "scenario W: an old kept server is flagged"
aged_home="$tmp/aged-home"
run_harness scenarioW "" env "${create_env[@]}" "${fast_waits[@]}" SERVERPRO_DOGFOOD_KEEP_SERVER=1 \
	SERVERPRO_DOGFOOD_HOME="$aged_home" SERVERPRO_DOGFOOD_SCENARIOS=create FAKE_CREATED_AT=2020-01-01T00:00:00.123456789Z
check "W age warning" grep -Fq "WARN | kept server age" "$scenario_tmp/harness.log"

note "scenario L: leftovers block a throwaway run before create"
run_harness scenarioL "" env "${create_env[@]}" FAKE_LEFTOVER=1
if [[ "$harness_rc" -ne 0 ]]; then ok "L nonzero exit"; else bad "L nonzero exit"; fi
check "L leftover reported" grep -Fq "FAIL | live leftover check preflight | servers already exist in hetzner/spdogfooda: old" "$scenario_tmp/harness.log"
check_absent "L create never attempted" "<server> <create>" "$FAKE_ARGV_DIR/serverpro-command.log"

note "scenario I: an interrupted run still deletes the throwaway server"
run_harness scenarioI "" env "${create_env[@]}" FAKE_INTERRUPT_ON_CREATE=1
if [[ "$harness_rc" -ne 0 ]]; then ok "I nonzero exit"; else bad "I nonzero exit"; fi
check "I interruption recorded" grep -Fq "INTERRUPTED | SIGTERM" "$scenario_tmp/harness.log"
check_command "I fallback delete ran" "CMD <-n> <spdogfooda> <-p> <hetzner> <--yes> <server> <delete> <web>"

run_harness scenarioI2 "" env "${create_env[@]}" FAKE_INTERRUPT_ON_CREATE=1 FAKE_SIGNAL_DURING_DELETE=1
check "I2 second signal does not abort the delete" grep -Fq "CLEANUP | deleted hetzner/spdogfooda/web" "$scenario_tmp/harness.log"
check "I2 leftover check runs on exit" grep -Fq "live leftover check exit" "$scenario_tmp/harness.log"

note "scenario K2: keep-mode home cannot alias the operator home"
for alias in "home/." "home//" "home/../home" "/"; do
	case "$alias" in
		/) alias_path="/" ;;
		*) alias_path="$tmp/scenarioK2-${alias//[^a-z]/_}/$alias" ;;
	esac
	run_harness "scenarioK2-${alias//[^a-z]/_}" "" env "${create_env[@]}" SERVERPRO_DOGFOOD_KEEP_SERVER=1 SERVERPRO_DOGFOOD_HOME="$alias_path"
	if [[ "$harness_rc" -eq 2 ]]; then ok "K2 alias $alias rejected"; else bad "K2 alias $alias rejected ($harness_rc)"; fi
	check_no_create_or_credentials "K2 alias $alias"
done

note "scenario T: a hung SSH read is cut off by the harness timeout"
started=$SECONDS
run_harness scenarioT "" env "${create_env[@]}" "${fast_waits[@]}" SERVERPRO_DOGFOOD_SSH_TIMEOUT=1 \
	SERVERPRO_DOGFOOD_SCENARIOS=create,power,delete FAKE_SSH_HANG=1
if [[ "$harness_rc" -ne 0 ]]; then ok "T nonzero exit"; else bad "T nonzero exit"; fi
check "T unreadable boot id reported" grep -Fq "FAIL | live boot id before restart | unreadable" "$scenario_tmp/harness.log"
if ((SECONDS - started < 15)); then ok "T bounded by SSH timeout"; else bad "T bounded by SSH timeout ($((SECONDS - started))s)"; fi

note "scenario J: identity failures fail loudly and still tear down the decoy"
run_harness scenarioJ-missing "" env "${create_env[@]}" "${fast_waits[@]}" \
	SERVERPRO_DOGFOOD_SCENARIOS=create,identity,delete FAKE_DECOY_MISSING=1
if [[ "$harness_rc" -ne 0 ]]; then ok "J missing decoy nonzero exit"; else bad "J missing decoy nonzero exit"; fi
check "J missing decoy reported" grep -Fq "FAIL | live identity decoy enrolled | only 1 device(s)" "$scenario_tmp/harness.log"
check "J decoy torn down after failure" grep -Fq "CLEANUP | identity decoy removed" "$scenario_tmp/harness.log"
run_harness scenarioJ-swap "" env "${create_env[@]}" "${fast_waits[@]}" \
	SERVERPRO_DOGFOOD_SCENARIOS=create,identity,delete FAKE_NODE_SWAP_ON_RERUN=1
if [[ "$harness_rc" -ne 0 ]]; then ok "J node swap nonzero exit"; else bad "J node swap nonzero exit"; fi
check "J node swap reported" grep -Fq "FAIL | live recorded node unchanged beside identity decoy | node-recorded became node-swapped" "$scenario_tmp/harness.log"
run_harness scenarioJ-join "" env "${create_env[@]}" "${fast_waits[@]}" \
	SERVERPRO_DOGFOOD_SCENARIOS=create,identity,delete FAKE_DECOY_JOIN_FAIL=1
if [[ "$harness_rc" -ne 0 ]]; then ok "J join failure nonzero exit"; else bad "J join failure nonzero exit"; fi
check "J join failure reported" grep -Fq "FAIL | live identity decoy enrolled | decoy tailscaled did not join" "$scenario_tmp/harness.log"

note "scenario M: import that exits zero with a failed row is rejected"
run_harness scenarioM "" env "${create_env[@]}" "${fast_waits[@]}" \
	SERVERPRO_DOGFOOD_SCENARIOS=create,import,delete FAKE_INVALID_SEMANTIC=import-status
if [[ "$harness_rc" -ne 0 ]]; then ok "M nonzero exit"; else bad "M nonzero exit"; fi
check "M import failure recorded" grep -Fq "FAIL | live server import" "$scenario_tmp/harness.log"

note "SUMMARY | fails=$fails"
[[ "$fails" -eq 0 ]]
