#!/usr/bin/env bash
# Tailnet device identity scenario sourced by test-dogfood-live.sh.
#
# WHY: create and doctor must keep acting on the device recorded for the
# server even when another online device shares its hostname and tag. The
# scenario enrols a short-lived decoy with exactly that identity on the test
# host itself, a second userspace tailscaled, so no extra paid machine is
# needed, then proves doctor and a create rerun stay bound to the recorded node.

DOGFOOD_TAILSCALE_API="https://api.tailscale.com/api/v2"
DOGFOOD_API_TIMEOUT_SECONDS=30
# The decoy key is single-use, ephemeral, and short-lived, so a leaked copy
# cannot enrol anything after the scenario and the device disappears on logout.
DOGFOOD_DECOY_KEY_EXPIRY_SECONDS=600
DOGFOOD_DECOY_DESCRIPTION="serverpro dogfood identity decoy"
# Root-only runtime directory on the test host; tmpfs, so a reboot clears it.
DOGFOOD_DECOY_DIR="/run/serverpro-dogfood-decoy"
DOGFOOD_DECOY_MIN_MATCHES=2
DOGFOOD_HOSTNAME_PATTERN='^[a-z0-9]([a-z0-9-]*[a-z0-9])?$'
DOGFOOD_TAILNET_PATTERN='^[A-Za-z0-9._@-]+$'
DOGFOOD_AUTH_KEY_PATTERN='^tskey-[A-Za-z0-9-]+$'
DOGFOOD_KEY_ID_PATTERN='^[A-Za-z0-9]+$'

decoy_key_id=""
decoy_started=0

# tailscale_api calls the Tailscale API with the dogfood token. The token goes
# through curl's stdin config, never argv, so it stays out of process lists.
tailscale_api() {
	local method="$1" path="$2" body="${3:-}"
	local -a args=(--silent --show-error --fail --max-time "$DOGFOOD_API_TIMEOUT_SECONDS" -X "$method" -K -)
	[[ -n "$body" ]] && args+=(-H "Content-Type: application/json" --data-binary "@$body")
	args+=("$DOGFOOD_TAILSCALE_API$path")
	printf 'header = "Authorization: Bearer %s"\n' "$SERVERPRO_DOGFOOD_TAILSCALE_TOKEN" | curl "${args[@]}"
}

# count_identity_devices counts tailnet devices that claim the managed
# hostname and tag; at least two means the decoy is really competing.
count_identity_devices() {
	local host="$1" tag="$2"
	tailscale_api GET "/tailnet/$SERVERPRO_DOGFOOD_TAILNET/devices" | python3 -c '
import json
import sys

host, tag = sys.argv[1:3]
devices = json.load(sys.stdin).get("devices", [])
# Offline devices, such as a decoy left by an earlier run, do not compete.
print(sum(
    1 for d in devices
    if d.get("hostname") == host
    and tag in (d.get("tags") or [])
    and (d.get("connectedToControl") or d.get("online"))
))
' "$host" "$tag"
}

# identity_teardown logs the decoy out (removing the ephemeral device), stops
# its daemon, and revokes the key. Registered as a finish hook so it also runs
# on failure or interruption, before any server delete.
identity_teardown() {
	if [[ "$decoy_started" -eq 1 ]]; then
		if remote_sudo_script >"$out_dir/identity-teardown.out" 2>"$out_dir/identity-teardown.err" <<SCRIPT
d='$DOGFOOD_DECOY_DIR'
if [ -S "\$d/sock" ]; then tailscale --socket="\$d/sock" logout || true; fi
if [ -f "\$d/pid" ]; then kill "\$(cat "\$d/pid")" 2>/dev/null || true; fi
rm -rf "\$d"
SCRIPT
		then
			log "CLEANUP | identity decoy removed"
		else
			log "CLEANUP-FAIL | identity decoy may remain on the test host under $DOGFOOD_DECOY_DIR"
		fi
		decoy_started=0
	fi
	if [[ -n "$decoy_key_id" ]]; then
		# A consumed single-use key may already be gone; revocation is best effort.
		tailscale_api DELETE "/tailnet/$SERVERPRO_DOGFOOD_TAILNET/keys/$decoy_key_id" >/dev/null 2>&1 || true
		decoy_key_id=""
	fi
}

# scenario_identity always tears the decoy down when it returns, while the
# server still exists; the finish hook only covers interruption mid-scenario.
scenario_identity() {
	local rc
	run_identity_checks
	rc=$?
	identity_teardown
	return "$rc"
}

run_identity_checks() {
	local host tag node_before node_after secret_dir body key count deadline
	host="$(state_field compute.name)"
	tag="tag:serverpro-$namespace"
	node_before="$(state_field tailscale.node_id)"
	# Values are interpolated into a root script, so only strict grammars pass.
	if [[ ! "$host" =~ $DOGFOOD_HOSTNAME_PATTERN || ! "$SERVERPRO_DOGFOOD_TAILNET" =~ $DOGFOOD_TAILNET_PATTERN || -z "$node_before" ]]; then
		log "FAIL | live identity preconditions | need recorded hostname, node id, and a valid tailnet name"
		fail=$((fail + 1))
		return 1
	fi

	secret_dir="$work_dir/secret"
	mkdir -p "$secret_dir" && chmod 700 "$secret_dir" || return 1
	body="$secret_dir/decoy-key-request.json"
	python3 - "$tag" "$DOGFOOD_DECOY_KEY_EXPIRY_SECONDS" "$DOGFOOD_DECOY_DESCRIPTION" >"$body" <<'PY'
import json
import sys

tag, expiry, description = sys.argv[1:4]
print(json.dumps({
    "capabilities": {"devices": {"create": {
        "reusable": False, "ephemeral": True, "preauthorized": True, "tags": [tag],
    }}},
    "expirySeconds": int(expiry),
    "description": description,
}))
PY
	finish_hooks+=(identity_teardown)
	if ! key="$(tailscale_api POST "/tailnet/$SERVERPRO_DOGFOOD_TAILNET/keys" "$body" | python3 -c '
import json
import sys

created = json.load(sys.stdin)
print(created["id"])
print(created["key"])
')"; then
		log "FAIL | live identity decoy key | Tailscale API key creation failed"
		fail=$((fail + 1))
		return 1
	fi
	decoy_key_id="${key%%$'\n'*}"
	key="${key#*$'\n'}"
	# The key is written into a root script, so only the documented shape passes.
	if [[ ! "$key" =~ $DOGFOOD_AUTH_KEY_PATTERN || ! "$decoy_key_id" =~ $DOGFOOD_KEY_ID_PATTERN ]]; then
		key=""
		log "FAIL | live identity decoy key | unexpected key format from the Tailscale API"
		fail=$((fail + 1))
		return 1
	fi

	decoy_started=1
	if ! remote_sudo_script >"$out_dir/identity-decoy.out" 2>"$out_dir/identity-decoy.err" <<SCRIPT
set -eu
d='$DOGFOOD_DECOY_DIR'
rm -rf "\$d"
install -d -m 700 "\$d"
umask 077
cat >"\$d/key" <<'SERVERPRO_DECOY_KEY'
$key
SERVERPRO_DECOY_KEY
nohup tailscaled --tun=userspace-networking --statedir="\$d/state" --socket="\$d/sock" --port=0 >"\$d/log" 2>&1 </dev/null &
echo \$! >"\$d/pid"
i=0
while [ ! -S "\$d/sock" ] && [ "\$i" -lt 30 ]; do i=\$((i + 1)); sleep 1; done
tailscale --socket="\$d/sock" up --auth-key="file:\$d/key" --hostname='$host' --advertise-tags='$tag' --timeout=60s
rm -f "\$d/key"
SCRIPT
	then
		key=""
		log "FAIL | live identity decoy enrolled | decoy tailscaled did not join"
		fail=$((fail + 1))
		return 1
	fi
	key=""

	deadline=$((SECONDS + recovery_timeout))
	count=0
	while ((SECONDS < deadline)); do
		count="$(count_identity_devices "$host" "$tag" 2>/dev/null)" || count=0
		((count >= DOGFOOD_DECOY_MIN_MATCHES)) && break
		sleep "$poll_interval"
	done
	if ((count < DOGFOOD_DECOY_MIN_MATCHES)); then
		log "FAIL | live identity decoy enrolled | only $count device(s) claim $host with $tag"
		fail=$((fail + 1))
		return 1
	fi
	log "PASS | live identity decoy enrolled | $count devices claim $host with $tag"
	pass=$((pass + 1))

	run_live_ok doctor-report "live doctor beside identity decoy" "$bin" --non-interactive -n "$namespace" -p "$provider" server doctor "$server"
	run_live_ok doctor-report "live create rerun beside identity decoy" "$bin" "${create_args[@]}"
	node_after="$(state_field tailscale.node_id)"
	if [[ "$node_after" == "$node_before" ]]; then
		log "PASS | live recorded node unchanged beside identity decoy"
		pass=$((pass + 1))
	else
		log "FAIL | live recorded node unchanged beside identity decoy | $node_before became ${node_after:-empty}"
		fail=$((fail + 1))
		return 1
	fi
}
