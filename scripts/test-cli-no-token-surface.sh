#!/usr/bin/env bash
set -u

# Exercises the full CLI surface that can run without live provider tokens,
# Tailscale keys, Cloudflare keys, SSH sessions, or paid infrastructure.
# The harness uses an isolated HOME so local-state mutations are intentional,
# inspectable, and discarded by default.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bin="${SERVERPRO_BIN:-${1:-./serverpro}}"
manifest="${SERVERPRO_CLI_SURFACE_MANIFEST:-$script_dir/cli-surface-dispositions.tsv}"
if [[ ! -x "$bin" ]]; then
	printf 'serverpro binary not executable: %s\n' "$bin" >&2
	exit 2
fi

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/serverpro-cli-surface.XXXXXX")"
home_dir="$work_dir/home"
out_dir="$work_dir/out"
results="$work_dir/results.txt"
mkdir -p "$home_dir" "$out_dir"

cleanup() {
	if [[ "${SERVERPRO_KEEP_HARNESS_TEMP:-}" == "1" ]]; then
		printf 'kept harness temp dir: %s\n' "$work_dir" >&2
		return
	fi
	rm -rf "$work_dir"
}
trap cleanup EXIT

export HOME="$home_dir"
unset SERVERPRO_SERVER_PROVIDER_TOKEN SERVER_PROVIDER_TOKEN
unset SERVERPRO_TAILSCALE_TOKEN TAILSCALE_API_TOKEN
unset SERVERPRO_CLOUDFLARE_TOKEN CLOUDFLARE_API_TOKEN

pass=0
fail=0
skip=0

log() {
	printf '%s\n' "$*" | tee -a "$results"
}

case_path() {
	printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '_'
}

run_ok() {
	local label="$1"
	shift
	local stem out err
	stem="$(case_path "$label")"
	out="$out_dir/$stem.out"
	err="$out_dir/$stem.err"
	if "$@" >"$out" 2>"$err"; then
		log "PASS | $label"
		pass=$((pass + 1))
	else
		log "FAIL | $label | exit $?"
		sed 's/^/  stdout: /' "$out" | tee -a "$results"
		sed 's/^/  stderr: /' "$err" | tee -a "$results"
		fail=$((fail + 1))
	fi
}

json_file_valid() {
	python3 - "$1" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    json.load(source)
PY
}

run_ok_grep() {
	local label="$1"
	local pattern="$2"
	shift 2
	local stem out err
	stem="$(case_path "$label")"
	out="$out_dir/$stem.out"
	err="$out_dir/$stem.err"
	if "$@" >"$out" 2>"$err" && json_file_valid "$out" 2>/dev/null && grep -Eq -- "$pattern" "$out"; then
		log "PASS | $label"
		pass=$((pass + 1))
	else
		log "FAIL | $label | invalid JSON or missing pattern: $pattern"
		sed 's/^/  stdout: /' "$out" | tee -a "$results"
		sed 's/^/  stderr: /' "$err" | tee -a "$results"
		fail=$((fail + 1))
	fi
}

run_ok_text_grep() {
	local label="$1"
	local pattern="$2"
	shift 2
	local stem out err
	stem="$(case_path "$label")"
	out="$out_dir/$stem.out"
	err="$out_dir/$stem.err"
	if "$@" >"$out" 2>"$err" && grep -Eq -- "$pattern" "$out"; then
		log "PASS | $label"
		pass=$((pass + 1))
	else
		log "FAIL | $label | missing pattern: $pattern"
		sed 's/^/  stdout: /' "$out" | tee -a "$results"
		sed 's/^/  stderr: /' "$err" | tee -a "$results"
		fail=$((fail + 1))
	fi
}

run_fail_grep() {
	local label="$1"
	local pattern="$2"
	shift 2
	local stem out err
	stem="$(case_path "$label")"
	out="$out_dir/$stem.out"
	err="$out_dir/$stem.err"
	if "$@" >"$out" 2>"$err"; then
		log "FAIL | $label | expected failure"
		sed 's/^/  stdout: /' "$out" | tee -a "$results"
		sed 's/^/  stderr: /' "$err" | tee -a "$results"
		fail=$((fail + 1))
	elif grep -Eq -- "$pattern" "$out" "$err"; then
		log "PASS | $label"
		pass=$((pass + 1))
	else
		log "FAIL | $label | failure missing pattern: $pattern"
		sed 's/^/  stdout: /' "$out" | tee -a "$results"
		sed 's/^/  stderr: /' "$err" | tee -a "$results"
		fail=$((fail + 1))
	fi
}

skip_case() {
	log "SKIP | $1 | $2"
	skip=$((skip + 1))
}

file_mode() {
	stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"
}

command_children() {
	local path="$1"
	local suffix="${path#serverpro}"
	local -a args=()
	if [[ -n "$suffix" ]]; then
		read -r -a args <<<"$suffix"
	fi
	"$bin" "${args[@]}" --help | awk '
		/^Available Commands:$/ { available = 1; next }
		available && /^  [[:alnum:]][[:alnum:]-]*[[:space:]]/ { print $1; next }
		available && !/^  / { exit }
	'
}

discover_commands() {
	local path="$1"
	local child
	printf '%s\n' "$path"
	while IFS= read -r child; do
		if [[ "$child" != "help" ]]; then
			discover_commands "$path $child"
		fi
	done < <(command_children "$path")
}

verify_command_inventory() {
	local mismatch
	mismatch="$(comm -3 \
		<(printf '%s\n' "$command_inventory" | LC_ALL=C sort) \
		<(awk -F '\t' '!/^#/ && NF { print $1 }' "$manifest" | LC_ALL=C sort))"
	if [[ -z "$mismatch" ]]; then
		log "PASS | command inventory has dispositions"
		pass=$((pass + 1))
	else
		log "FAIL | command inventory has dispositions"
		printf '%s\n' "$mismatch" | sed 's/^/  inventory: /' | tee -a "$results"
		fail=$((fail + 1))
	fi
}

verify_disposition_evidence() {
	local path label missing=0
	while IFS=$'\t' read -r path label; do
		if [[ -z "$path" || "$path" == \#* ]]; then
			continue
		fi
		if ! grep -Fqx "PASS | $label" "$results"; then
			log "FAIL | missing disposition evidence | $path | $label"
			missing=1
		fi
	done <"$manifest"
	if [[ "$missing" -eq 0 ]]; then
		log "PASS | command dispositions have passing evidence"
		pass=$((pass + 1))
	else
		fail=$((fail + 1))
	fi
}

verify_recovery_env_scrubbed() {
	local name leaked=0
	for name in SERVERPRO_SERVER_PROVIDER_TOKEN SERVER_PROVIDER_TOKEN SERVERPRO_TAILSCALE_TOKEN TAILSCALE_API_TOKEN SERVERPRO_CLOUDFLARE_TOKEN CLOUDFLARE_API_TOKEN; do
		if printenv "$name" >/dev/null 2>&1; then
			log "FAIL | recovery token environment scrubbed | $name"
			leaked=1
		fi
	done
	if [[ "$leaked" -eq 0 ]]; then
		log "PASS | recovery token environment scrubbed"
		pass=$((pass + 1))
	else
		fail=$((fail + 1))
	fi
}

verify_json_parser() {
	local valid="$out_dir/json-parser-valid.out"
	local noisy="$out_dir/json-parser-noisy.out"
	printf '{"status":"ok"}\n' >"$valid"
	printf '{"status":"ok"}\nnoise\n' >"$noisy"
	if json_file_valid "$valid" && ! json_file_valid "$noisy" 2>/dev/null; then
		log "PASS | JSON parser rejects noisy stdout"
		pass=$((pass + 1))
	else
		log "FAIL | JSON parser rejects noisy stdout"
		fail=$((fail + 1))
	fi
}

run_command_path() {
	local label="$1"
	local path="$2"
	shift 2
	local suffix="${path#serverpro}"
	local -a args=()
	if [[ -n "$suffix" ]]; then
		read -r -a args <<<"$suffix"
	fi
	# Keep one path-to-argv conversion so discovery, help, and parent checks cannot drift.
	run_ok "$label" "$bin" "${args[@]}" "$@"
}

command_has_children() {
	local path="$1"
	awk -v prefix="$path " 'index($0, prefix) == 1 { found = 1 } END { exit !found }' <<<"$command_inventory"
}

command_inventory="$(discover_commands serverpro)"

log "serverpro CLI no-token surface run"
log "binary: $bin"
"$bin" --version | tee -a "$results"
shasum -a 256 "$bin" | tee -a "$results"
verify_recovery_env_scrubbed
verify_command_inventory
verify_json_parser

while IFS= read -r path; do
	run_command_path "help: $path --help" "$path" --help
done <<<"$command_inventory"
# Cobra's built-in help command is intentionally absent from the disposition manifest.
run_ok "help: serverpro help" "$bin" help

while IFS= read -r path; do
	if [[ "$path" != "serverpro" ]] && command_has_children "$path"; then
		run_command_path "parent help: ${path#serverpro }" "$path"
	fi
done <<<"$command_inventory"

run_ok_text_grep "server bootstrap help names managed toolset" 'Node 24\.20\.0.*npm 11\.19\.0.*Pi 0\.84\.3.*uv 0\.12\.6.*Rust 1\.98\.0.*Herdr 0\.8\.2.*gh 2\.98\.0' "$bin" server bootstrap --help

run_ok "root no args shows help" "$bin"
run_ok "version flag" "$bin" --version
run_fail_grep "invalid global timeout" 'time: invalid duration' "$bin" --timeout nope doctor
run_ok "global timeout parses" "$bin" --timeout 1s doctor
run_fail_grep "tailnet reconcile token guard" 'SERVERPRO_TAILSCALE_TOKEN required' "$bin" --non-interactive --dry-run tailnet reconcile example.ts.net

run_ok_grep "namespace list empty" '^\[\]$' "$bin" namespace list
run_ok_grep "namespace create mynamespace" '"status": "created"' "$bin" namespace create mynamespace
run_ok_grep "namespace list after create" '"namespace": "mynamespace"' "$bin" namespace list
run_ok_grep "namespace status mynamespace" '"namespace": "mynamespace"' "$bin" namespace status mynamespace
run_fail_grep "namespace create invalid" 'invalid namespace "BadName"' "$bin" namespace create BadName
run_fail_grep "namespace status missing" 'namespace "missingns" not found' "$bin" namespace status missingns

cfg_dir="$HOME/.config/serverpro/namespaces/mynamespace"
st_dir="$HOME/.local/state/serverpro/namespaces/mynamespace"
if [[ -d "$cfg_dir" && -d "$st_dir" && "$(file_mode "$cfg_dir")" == "700" && "$(file_mode "$st_dir")" == "700" ]]; then
	log "PASS | namespace directories mode 0700"
	pass=$((pass + 1))
else
	log "FAIL | namespace directories mode 0700"
	fail=$((fail + 1))
fi

run_ok_grep "doctor global" '"scope": "providers"' "$bin" doctor
run_fail_grep "doctor --fix guard" '--fix is only supported' "$bin" doctor --fix
run_ok_grep "provider list" 'digitalocean|hetzner|vultr' "$bin" provider list
for provider in digitalocean hetzner vultr; do
	run_ok_grep "provider status $provider" "\"name\": \"$provider\"" "$bin" provider status "$provider"
done
run_fail_grep "provider status invalid" 'provider "unknown" not found' "$bin" provider status unknown
for provider in digitalocean hetzner vultr; do
	run_fail_grep "provider doctor $provider token guard" 'SERVERPRO_SERVER_PROVIDER_TOKEN required' "$bin" --non-interactive provider doctor "$provider"
done

run_fail_grep "location list missing provider" '--provider/-p is required for "serverpro location list"' "$bin" --non-interactive location list
for provider in digitalocean hetzner vultr; do
	run_fail_grep "location list $provider token guard" 'SERVERPRO_SERVER_PROVIDER_TOKEN required' "$bin" --non-interactive -p "$provider" location list
	run_fail_grep "size list $provider token guard" 'SERVERPRO_SERVER_PROVIDER_TOKEN required' "$bin" --non-interactive -p "$provider" size list --location test
	run_fail_grep "image list $provider token guard" 'SERVERPRO_SERVER_PROVIDER_TOKEN required' "$bin" --non-interactive -p "$provider" image list --location test
done

run_fail_grep "server discover missing provider" '--provider/-p is required for "serverpro server discover"' "$bin" --non-interactive server discover
run_fail_grep "server discover token guard" 'SERVERPRO_SERVER_PROVIDER_TOKEN required' "$bin" --non-interactive -p hetzner server discover
run_fail_grep "server import missing criteria" 'import requires NAME, --provider-id, or --all' "$bin" --non-interactive --dry-run -p hetzner server import
run_fail_grep "server import missing provider" '--provider/-p is required for "serverpro server import"' "$bin" --non-interactive --dry-run server import webapp
run_fail_grep "server import token guard" 'SERVERPRO_SERVER_PROVIDER_TOKEN required' "$bin" --non-interactive --dry-run -p hetzner server import webapp

run_ok_grep "server create dry-run hetzner" 'size=cx23 image=ubuntu-24.04 location=fsn1' "$bin" --non-interactive --dry-run -n previewns -p hetzner server create previewapp --location fsn1 --size cx23 --image ubuntu-24.04
run_ok_grep "server create dry-run vultr" 'size=vc2-1c-1gb image=1743 location=ewr' "$bin" --non-interactive --dry-run -n previewns -p vultr server create previewapp --location ewr --size vc2-1c-1gb --image 1743
run_ok_grep "server create dry-run digitalocean" 'size=s-1vcpu-1gb image=ubuntu-24-04-x64 location=nyc3' "$bin" --non-interactive --dry-run -n previewns -p digitalocean server create previewapp --location nyc3 --size s-1vcpu-1gb --image ubuntu-24-04-x64
run_ok_grep "server create dry-run all flags" 'cloudflare tunnel' "$bin" --non-interactive --dry-run -n previewns -p hetzner server create previewapp --compute-name previewns-previewapp --location fsn1 --size cx23 --image ubuntu-24.04 --admin-user ops --tailscale-tailnet example.ts.net --tailscale-tags tag:serverpro-previewns --ingress cloudflare-tunnel --cloudflare-account-id acct-test --cloudflare-tunnel-name previewns-previewapp --egress-mode open
run_fail_grep "server create missing provider" '--provider/-p is required for "serverpro server create"' "$bin" --non-interactive --dry-run -n previewns server create previewapp --location fsn1 --size cx23 --image ubuntu-24.04
run_fail_grep "server create invalid provider" 'provider "digital ocean" not found' "$bin" --non-interactive --dry-run -n previewns -p 'digital ocean' server create previewapp --location nyc3 --size s-1vcpu-1gb --image ubuntu-24-04-x64
run_fail_grep "server create missing location" 'missing required config: compute.location' "$bin" --non-interactive --dry-run -n previewns -p hetzner server create previewapp --size cx23 --image ubuntu-24.04
run_fail_grep "server create unsupported ingress" 'network.ingress must be none or cloudflare-tunnel' "$bin" --non-interactive --dry-run -n previewns -p hetzner server create previewapp --location fsn1 --size cx23 --image ubuntu-24.04 --ingress tailscale-funnel

if find "$HOME" -path '*previewapp*' | grep . >/dev/null; then
	log "FAIL | server create dry-runs wrote previewapp files"
	find "$HOME" -path '*previewapp*' | tee -a "$results"
	fail=$((fail + 1))
else
	log "PASS | server create dry-runs wrote no previewapp files"
	pass=$((pass + 1))
fi

ns="mynamespace"
server="webapp"
servers_cfg_dir="$HOME/.config/serverpro/namespaces/$ns/servers"
servers_st_dir="$HOME/.local/state/serverpro/namespaces/$ns/servers"
registry_path="$HOME/.local/state/serverpro/registry.json"
config_path="$servers_cfg_dir/$server.yaml"
state_path="$servers_st_dir/$server.json"
credentials_path="$servers_cfg_dir/$server/credentials.json"
mkdir -p "$servers_cfg_dir" "$servers_st_dir"
cat >"$config_path" <<'YAML'
namespace: mynamespace
server: webapp
compute:
  location: fsn1
  size: cx23
  image: ubuntu-24.04
admin:
  username: ops
network:
  ingress: none
  egress:
    mode: restricted
access:
  tailscale:
    enabled: true
    ssh: true
    tailnet: example.ts.net
    tags:
      - tag:serverpro-mynamespace
hardening:
  profile: strict
  unattended_upgrades: true
  apparmor: true
  ufw: true
  journald_persistent: true
YAML
cat >"$state_path" <<'JSON'
{
  "schema_version": 1,
  "namespace": "mynamespace",
  "server": "webapp",
  "compute": {
    "provider": "hetzner",
    "namespace": "mynamespace",
    "server": "webapp",
    "id": "srv-test",
    "name": "mynamespace-webapp",
    "location": "fsn1",
    "size": "cx23",
    "image": "ubuntu-24.04",
    "public_ipv4": "192.0.2.10",
    "provider_state": {"access_policy_id": "fw-test"}
  },
  "tailscale": {
    "tailnet": "example.ts.net",
    "node_id": "node-test",
    "auth_key_id": "key-test",
    "name": "webapp.test.ts.net",
    "ips": ["100.64.0.10"],
    "tags": ["tag:serverpro-mynamespace"],
    "policy_tag_owners": ["tag:serverpro-mynamespace"],
    "policy_ssh_rule": true,
    "policy_ssh_tags": ["tag:serverpro-mynamespace"]
  },
  "cloudflare": {"tunnel_id": "tun-test", "name": "mynamespace-webapp"},
  "labels": {"managed-by": "serverpro", "serverpro-namespace": "mynamespace", "serverpro-server": "webapp"},
  "created_at": "2026-06-29T00:00:00Z",
  "updated_at": "2026-06-29T00:00:00Z"
}
JSON
cat >"$registry_path" <<JSON
{
  "schema_version": 1,
  "namespaces": {
    "mynamespace": {
      "servers": {
        "webapp": {
          "namespace": "mynamespace",
          "server": "webapp",
          "state_path": "$state_path",
          "config_path": "$config_path",
          "credentials_path": "$credentials_path",
          "resource_names": {"compute_server": "mynamespace-webapp", "cloudflare_tunnel": "mynamespace-webapp"},
          "labels": {"managed-by": "serverpro", "serverpro-namespace": "mynamespace", "serverpro-server": "webapp"},
          "created_at": "2026-06-29T00:00:00Z",
          "updated_at": "2026-06-29T00:00:00Z"
        }
      }
    }
  },
  "updated_at": "2026-06-29T00:00:00Z"
}
JSON
chmod 700 "$HOME/.config/serverpro/namespaces/$ns" "$HOME/.local/state/serverpro/namespaces/$ns" "$servers_cfg_dir" "$servers_st_dir"

run_ok_grep "server list fixture" '"server": "webapp"' "$bin" server list
run_ok_grep "server list namespace filter" '"namespace": "mynamespace"' "$bin" -n mynamespace server list
run_ok_grep "server status --all no matches" '^\[\]$' "$bin" --non-interactive --all server status missing
run_fail_grep "server status token guard" 'missing credentials: \[server provider API token\]' "$bin" --non-interactive -n mynamespace server status webapp
run_ok_grep "server doctor dry-run" '"action": "doctor"' "$bin" --non-interactive --dry-run -n mynamespace server doctor webapp
run_fail_grep "server doctor fix dry-run guard" '--fix cannot be used with --dry-run' "$bin" --non-interactive --dry-run -n mynamespace server doctor webapp --fix
run_fail_grep "server doctor token guard" 'missing credentials: \[server provider API token' "$bin" --non-interactive -n mynamespace server doctor webapp
run_ok_grep "server ssh dry-run" 'tailscale' "$bin" --non-interactive --dry-run -n mynamespace server ssh webapp
skip_case "server ssh live" "requires tailscale SSH/session and remote host"

for target in all git docker mise node pi; do
	run_ok_grep "server bootstrap dry-run $target" "\"target\": \"$target\"" "$bin" --non-interactive --dry-run -n mynamespace server bootstrap webapp "$target"
done
run_fail_grep "server bootstrap invalid target" 'unsupported bootstrap target' "$bin" --non-interactive --dry-run -n mynamespace server bootstrap webapp badtarget
skip_case "server bootstrap live" "requires remote sudo password and tailscale SSH"

for action in start stop restart; do
	run_ok_grep "server $action dry-run" "\"action\": \"$action\"" "$bin" --non-interactive --dry-run -n mynamespace server "$action" webapp
	skip_case "server $action live" "requires provider credentials"
done
run_ok_grep "server delete dry-run" '"external_cleanup"' "$bin" --non-interactive --dry-run -n mynamespace server delete webapp
skip_case "server delete live" "destructive and requires provider credentials"
run_ok_grep "namespace delete dry-run" '"status": "planned"' "$bin" --non-interactive --dry-run namespace delete mynamespace
if [[ -f "$state_path" && -f "$config_path" && -d "$cfg_dir" && -d "$st_dir" ]]; then
	log "PASS | namespace delete dry-run wrote nothing"
	pass=$((pass + 1))
else
	log "FAIL | namespace delete dry-run wrote nothing"
	fail=$((fail + 1))
fi

run_ok_grep "explicit config/state doctor dry-run" '"action": "doctor"' "$bin" --non-interactive --dry-run --config "$config_path" --state "$state_path" server doctor webapp
run_ok_grep "explicit state delete dry-run" '"action": "delete"' "$bin" --non-interactive --dry-run --state "$state_path" server delete webapp

run_ok_grep "ingress list empty" '^\[\]$' "$bin" --non-interactive -n mynamespace server ingress list webapp
run_fail_grep "ingress add missing type" '--type is required for "serverpro server ingress add"' "$bin" --non-interactive -n mynamespace server ingress add webapp --hostname app.example.com
run_fail_grep "ingress add missing hostname" '--hostname is required for "serverpro server ingress add"' "$bin" --non-interactive -n mynamespace server ingress add webapp --type cloudflare-tunnel
run_fail_grep "ingress add unsupported type" 'unsupported ingress type "none"' "$bin" --non-interactive -n mynamespace server ingress add webapp --type none --hostname app.example.com
run_ok_grep "ingress add cloudflare-tunnel" '"status": "added"' "$bin" --non-interactive -n mynamespace server ingress add webapp --type cloudflare-tunnel --hostname app.example.com
run_ok_grep "ingress list after add" 'app.example.com' "$bin" --non-interactive -n mynamespace server ingress list webapp
run_fail_grep "ingress add duplicate" 'ingress hostname "app.example.com" already exists' "$bin" --non-interactive -n mynamespace server ingress add webapp --type cloudflare-tunnel --hostname app.example.com
run_ok_grep "ingress remove" '"status": "removed"' "$bin" --non-interactive -n mynamespace server ingress remove webapp --hostname app.example.com
run_ok_grep "ingress list after remove" '^\[\]$' "$bin" --non-interactive -n mynamespace server ingress list webapp
run_fail_grep "ingress remove missing hostname" '--hostname is required for "serverpro server ingress remove"' "$bin" --non-interactive -n mynamespace server ingress remove webapp
run_fail_grep "ingress remove missing route" 'ingress hostname "missing.example.com" not found' "$bin" --non-interactive -n mynamespace server ingress remove webapp --hostname missing.example.com

if find "$HOME" -type f | grep -E 'credentials\.json|previewapp|previewns' >/dev/null; then
	log "FAIL | unwanted credential/preview files"
	find "$HOME" -type f | grep -E 'credentials\.json|previewapp|previewns' | tee -a "$results"
	fail=$((fail + 1))
else
	log "PASS | no credentials or preview files created"
	pass=$((pass + 1))
fi

verify_disposition_evidence
log "SUMMARY | pass=$pass fail=$fail skip=$skip"
if [[ "$fail" -ne 0 ]]; then
	exit 1
fi
