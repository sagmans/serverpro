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

args=("$@")
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
		else
			printf '[]\n'
		fi
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
		status=pass
		[[ "${FAKE_INVALID_SEMANTIC:-}" == create-doctor-status ]] && status=fail
		printf '{"results":[{"name":"selftest","scope":"fake","status":"%s","evidence":"ok"}]}\n' "$status"
		;;
	*" server status "*)
		server="$(sequence_value server status)"
		power=running
		case "${FAKE_INVALID_SEMANTIC:-}" in
			status-provider) provider=wrong ;;
			status-power) power= ;;
		esac
		printf '{"namespace":"%s","server":"%s","provider":"%s","power":"%s"}\n' "$namespace" "$server" "$provider" "$power"
		;;
	*" server doctor "*)
		status=pass
		[[ "${FAKE_INVALID_SEMANTIC:-}" == server-doctor-status ]] && status=fail
		printf '{"results":[{"name":"selftest","scope":"fake","status":"%s","evidence":"ok"}]}\n' "$status"
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

chmod +x "$fakebin/python3" "$fakebin/serverpro"

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
	mkdir -p "$FAKE_ARGV_DIR" "$stmp/htmp"
	[[ -z "$delete_status" ]] || printf '%s\n' "$delete_status" >"$FAKE_ARGV_DIR/delete-status"
	env -i PATH="$fakebin:/usr/bin:/bin" HOME="$stmp/home" \
		TMPDIR="$stmp/htmp" FAKE_ARGV_DIR="$FAKE_ARGV_DIR" FAKE_REAL_PYTHON="$real_python" \
		SERVERPRO_BIN="$fakebin/serverpro" \
		SENT_HETZNER="$SENT_HETZNER" SENT_VULTR="$SENT_VULTR" \
		SENT_DIGITALOCEAN="$SENT_DIGITALOCEAN" SENT_CF="$SENT_CF" \
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

note "SUMMARY | fails=$fails"
[[ "$fails" -eq 0 ]]
