# Usage

serverpro manages hardened Ubuntu servers through provider adapters. The CLI is
resource-first and provider-neutral. A namespace is the top-level serverpro
resource group; server credentials are scoped to one server inside that
namespace.

## Command grammar

Every command follows one shape:

```text
serverpro GROUP [SUBGROUP] VERB [TARGET] [--scope-flags] [--flags]
```

The target is positional and is the resource the verb acts on (`server status
webapp`, `provider status hetzner`, `tailnet reconcile example.ts.net`).
Namespace and provider are scope flags (`-n`, `-p`), never positional outside
their own command groups. Flags filter or set options; they never name the
target. Per-server subresources nest under `server` (for example `serverpro
server ingress add webapp`).

## Supported runtime

The supported controller is macOS 27 arm64. Managed hosts must be Ubuntu 24.04
LTS (`noble`) on amd64 or arm64. Live create refetches the selected location's
catalog and rejects unsupported images before provider mutation. Bootstrap,
Tailscale repair, and Cloudflare Tunnel installation validate the actual host
before managed mutation; cloud-init repeats the check as defense in depth.
Provider image IDs differ, and an arbitrary explicit `--image` value does not
widen support. `INSTALLATION.md` owns the full tool and package version matrix.

## Global flags

```text
--config <path>          config override: server create, server doctor, and server bootstrap
--state <path>           state override: supported server and server ingress commands
-n, --namespace <id>     serverpro namespace
-p, --provider <name>    compute provider, e.g. hetzner, vultr, or digitalocean
-A, --all                operate on every matching resource where supported
--dry-run                preview without mutation
--non-interactive        disable prompts; fail when required input is absent
-y, --yes                approve live/destructive commands
--timeout <duration>     operation timeout, including workflow-lock waits
```

Persistent flags are command-scoped; each command's `--help` shows only flags
that command accepts. `--config` is accepted only by
`server create`, `server doctor`, and `server bootstrap`. `--state` is accepted only by:

- `server create`, `server status`, `server doctor`, `server ssh`
- `server delete`, `server start`, `server stop`, `server restart`
- `server bootstrap`, `server ingress add`, `server ingress list`, and `server ingress remove`

Command results are JSON on stdout except live `server ssh`, which hands
terminal I/O directly to `tailscale ssh`. Prompts and confirmations go to
stderr. Long create/bootstrap stages also emit secret-safe stderr events such
as `progress phase=provision elapsed=12s attempt=1`; fixed phase names never
include tokens, passwords, hashes, bootstrap data, or target values. Create uses
independent timeout budgets for provisioning and doctor. Interactive GitHub
prompts do not consume the doctor timeout.

Interactive commands resolve missing inputs when safe:

- One matching namespace, provider, or server is selected automatically.
- Multiple matches use `fzf` when available.
- Without `fzf`, serverpro prints numbered choices.
- `--all` displays all matching resources instead of prompting.
- `--non-interactive` never prompts; missing flags, defaults, or env values
  fail fast with the exact input to pass.
- `-y/--yes` is separate: it approves live/destructive commands when prompts
  are disabled or skipped.

## Resource commands

```text
serverpro namespace create NAME
serverpro namespace list
serverpro namespace status NAME
serverpro namespace delete NAME

serverpro server create NAME
serverpro server bootstrap NAME [all|git|docker|mise|node|pi]
serverpro server list
serverpro server status NAME
serverpro server doctor NAME
serverpro server ssh NAME
serverpro server discover -p PROVIDER [--server NAME] [--provider-id ID] [--include-unmanaged]
serverpro server import [NAME]
serverpro server start|stop|restart NAME
serverpro server delete NAME

serverpro provider list
serverpro provider status NAME
serverpro provider doctor NAME

serverpro location list -p PROVIDER
serverpro size list -p PROVIDER --location LOCATION
serverpro image list -p PROVIDER --location LOCATION

serverpro server ingress list SERVER
serverpro server ingress add SERVER --type cloudflare-tunnel --hostname HOSTNAME
serverpro server ingress remove SERVER --hostname HOSTNAME

serverpro tailnet reconcile TAILNET
```

## Namespaces and server credentials

A namespace groups local config, state, names, tags, and ownership metadata for
related resources. Secrets are not global and are not namespace-wide: each
server has its own credential file with server provider, Tailscale, and
Cloudflare tokens.

```sh
serverpro namespace create mynamespace
serverpro namespace list
```

Passing `-n/--namespace` selects that namespace and skips the namespace prompt:

```sh
serverpro server status webapp -n mynamespace
```

Provider-visible ownership metadata uses one convention across Hetzner, Vultr,
and DigitalOcean. Hetzner stores labels with `=` separators:

```text
managed-by=serverpro
serverpro-namespace=<namespace>
serverpro-server=<server>
```

Vultr and DigitalOcean store the same keys as colon-delimited tags:

```text
managed-by:serverpro
serverpro-namespace:<namespace>
serverpro-server:<server>
```

For flat tag providers, values with provider-invalid characters are encoded
reversibly before submission and decoded for ownership checks.

### Supported server config schema

Config loading uses strict known-field decoding. Only these keys are supported:

```yaml
namespace: mynamespace
server: webapp
credentials:
  json_path: ~/.config/serverpro/namespaces/mynamespace/servers/webapp/credentials.json
compute:
  name: mynamespace-webapp
  location: fsn1
  size: cx23
  image: ubuntu-24.04
  labels:
    custom-label: value
admin:
  username: deploy
  store_console_password: false
network:
  ingress: none
  egress:
    mode: restricted
    phase_lockdown_after_bootstrap: true
access:
  public_ssh: false
  tailscale:
    enabled: true
    ssh: true
    tailnet: '-'
    tags: [tag:serverpro-mynamespace]
    root_policy: check-or-disabled
cloudflare:
  account_id: ''
  tunnel:
    enabled: false
    name: mynamespace-webapp
    create_connector_only: false
hardening:
  profile: strict
  unattended_upgrades: true
  apparmor: true
  ufw: true
  journald_persistent: true
git:
  identity:
    name: ""
    email: ""
  signing: false
  access: none
  deploy_repository: ""
```

`network.egress.mode: restricted` permits only DNS, NTP, HTTP(S), outbound
SSH (22/tcp, for git-over-SSH), Cloudflare Tunnel, and Tailscale egress;
`open` allows all outbound traffic.

The optional `git` section records durable GitHub setup intent chosen during
interactive create/bootstrap: `access: none` leaves git untouched,
`deploy-key` is the read-only single-repo flow and requires the normalized
`deploy_repository`, and `account-key` is full development access (account SSH
key, exact git identity, optional SSH commit signing, and required gh CLI PAT
auth). Non-secret intent is saved before remote mutation so an interrupted flow
remains diagnosable. The PAT and private keys are never written to local config
or state; gh stores the PAT only on the managed host. A successful deploy-key to
account-key transition removes the exact managed repository rewrite and SSH
block, then clears `deploy_repository`. `serverpro server doctor` re-checks the
account-key setup against the exact configured identity and signing state and
fixes config-only drift.

Account-key setup currently manages one GitHub development profile per server:
one global Git author identity, one account SSH key, one optional signing key,
and one stored `gh` token. A fine-grained PAT belongs to exactly one resource
owner, so `all repositories` covers only the selected personal account or
organization. Git-over-SSH follows the account key's full account access, while
`gh` API access remains limited to the PAT resource owner. Multiple GitHub
usernames and multiple PAT resource-owner profiles are not managed.

Legacy config files containing only `project` still load, but every save rewrites
that identity as `namespace`. Files containing both fields must use the same
value; divergent identities are rejected. Legacy credential and state JSON
`project` keys and registry `projects` objects are likewise read-only migration
inputs; subsequent writes use only `namespace` and `namespaces`.

Previously accepted `credentials.mode`, `network.egress.allow`,
`access.emergency_ssh`, and `cloudflare.tunnel.smoke_route` keys never controlled
runtime behavior and are now rejected. Remove them from existing config files;
use `network.egress.mode`, provider rescue-console guidance, and explicit
`serverpro server ingress` commands instead.

## Create a server

Preview:

```sh
serverpro server create webapp \
  -n mynamespace -p hetzner \
  --location fsn1 \
  --size cx23 \
  --image ubuntu-24.04 \
  --dry-run
```

Live create:

```sh
serverpro server create webapp \
  -n mynamespace -p hetzner \
  --location fsn1 \
  --size cx23 \
  --image ubuntu-24.04
```

Create requires explicit provider, location, size, and image values. Use
`serverpro location list -p PROVIDER`, `serverpro size list -p PROVIDER`, and
`serverpro image list -p PROVIDER` first. Select a supported Ubuntu 24.04 image.
Live
create verifies that exact image against the current selected-location catalog
before the first provider mutation. During live create, every externally visible policy, tunnel, auth-key, compute, and device
mutation is checkpointed. A failed create reports its lifecycle phase and known
non-secret resource IDs; rerun create to resume from durable checkpoints or use
`server delete` to clean tracked resources, including access policies recorded
before a compute server ID exists. A retry refetches checkpointed provider access
policy and fails before compute mutation if ownership, rules/selectors, or
attachments broadened. If a tunnel was created before its checkpoint was
published, rerun adopts the one exact-name tunnel instead of creating a
duplicate; multiple exact matches fail as ambiguous. A definitive checkpoint
failure deletes only the tunnel created by that attempt, never an adopted one.
Durable state records whether each tunnel was created, adopted, or imported;
delete removes only tunnels proven created by serverpro. Legacy state without
provenance fails closed and retains the tunnel. Create prompts for missing server-scoped
secrets and stores them at:

```text
~/.config/serverpro/namespaces/<namespace>/servers/<server>/credentials.json
```

Every prompted config value can also be passed as a flag:

```text
--compute-name NAME
--location LOCATION
--size SIZE
--image IMAGE
--admin-user USER
--tailscale-tailnet TAILNET
--tailscale-tags TAG[,TAG...]
--ingress none|cloudflare-tunnel
--cloudflare-account-id ACCOUNT_ID
--cloudflare-tunnel-name NAME
--egress-mode restricted|open
```

Use interactive create when running by hand so serverpro can prompt for missing
secrets. Non-interactive create is for fully scripted runs; it requires the sudo
password env var plus an existing server-scoped credential file:

```sh
MYNAMESPACE_WEBAPP_SUDOPASS='use-a-long-remote-admin-password' \
  serverpro server create webapp \
  -n mynamespace -p hetzner \
  --location fsn1 \
  --size cx23 \
  --image ubuntu-24.04 \
  --non-interactive --yes
```

## Catalog and provider checks

`location list`, `size list`, `image list`, and `provider doctor` run before a
server credential file may exist, so they use an ephemeral server provider
token. Interactive runs prompt for it; non-interactive runs read
`SERVERPRO_SERVER_PROVIDER_TOKEN`.

```sh
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro location list -p hetzner --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro location list -p vultr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro location list -p digitalocean --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro provider doctor hetzner --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro provider doctor vultr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro provider doctor digitalocean --non-interactive
```

The Vultr adapter uses numeric OS IDs for `--image`; choose the current Ubuntu
24.04 entry shown by `serverpro image list -p vultr`. The DigitalOcean
adapter uses image slugs such as `ubuntu-24-04-x64` or its arm64 counterpart.
The token is used
for that command only and is never stored globally.

### Creating a Vultr server

```sh
# 1. Get your Vultr API token from https://my.vultr.com/settings/#settingsapi
# 2. Check available locations/plans/images
SERVERPRO_SERVER_PROVIDER_TOKEN='vultr-api-token' \
  serverpro location list -p vultr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='vultr-api-token' \
  serverpro size list -p vultr --location ewr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='vultr-api-token' \
  serverpro image list -p vultr --location ewr --non-interactive

# 3. Preview a create
serverpro server create webapp \
  -n mynamespace -p vultr \
  --location ewr \
  --size vc2-1c-1gb \
  --image 1743 \
  --dry-run

# 4. Run live create; Cloudflare is requested only when ingress is enabled
serverpro server create webapp \
  -n mynamespace -p vultr \
  --location ewr \
  --size vc2-1c-1gb \
  --image 1743

# Or non-interactive with env secrets
MYNAMESPACE_WEBAPP_SUDOPASS='use-a-long-remote-admin-password' \
  serverpro server create webapp \
  -n mynamespace -p vultr \
  --location ewr \
  --size vc2-1c-1gb \
  --image 1743 \
  --tailscale-tailnet mytailnet.ts.net \
  --tailscale-tags tag:serverpro-mynamespace \
  --non-interactive --yes
```

Only IPv4-backed instances are created (`enable_ipv6=false`). A deny-by-default
Vultr firewall group permits Tailscale WireGuard (`41641/udp`) and STUN
(`3478/udp`) on IPv4 and IPv6. Ownership uses the shared provider metadata
convention.

### Creating a DigitalOcean server

```sh
# 1. Get your DigitalOcean API token from https://cloud.digitalocean.com/account/api/tokens
# 2. Check available regions/sizes/images
SERVERPRO_SERVER_PROVIDER_TOKEN='digitalocean-api-token' \
  serverpro location list -p digitalocean --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='digitalocean-api-token' \
  serverpro size list -p digitalocean --location nyc3 --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='digitalocean-api-token' \
  serverpro image list -p digitalocean --location nyc3 --non-interactive

# 3. Preview a create
serverpro server create webapp \
  -n mynamespace -p digitalocean \
  --location nyc3 \
  --size s-1vcpu-1gb \
  --image ubuntu-24-04-x64 \
  --dry-run

# 4. Run live create; Cloudflare is requested only when ingress is enabled
serverpro server create webapp \
  -n mynamespace -p digitalocean \
  --location nyc3 \
  --size s-1vcpu-1gb \
  --image ubuntu-24-04-x64
```

Only IPv4-backed droplets are created (`ipv6=false`). DigitalOcean tag
resources are ensured first, then a tag-bound firewall is created before the
droplet. It keeps SSH closed, allows Tailscale UDP ports `41641` and `3478`
inbound, and allows outbound traffic. Ownership uses the shared provider
metadata convention. The token needs tag read/create scope in addition to
droplet and firewall scopes.

## Inspect and operate

```sh
serverpro server list
serverpro server status webapp -n mynamespace
serverpro server doctor webapp -n mynamespace
serverpro server bootstrap webapp all -n mynamespace
serverpro server bootstrap webapp git -n mynamespace
serverpro server ssh webapp -n mynamespace
serverpro server start webapp -n mynamespace -p hetzner --yes
serverpro server stop webapp -n mynamespace -p hetzner --yes
serverpro server restart webapp -n mynamespace -p hetzner --yes
```

## Recover local state from provider labels

If `~/.config/serverpro` or `~/.local/state/serverpro` is lost but labeled VMs
still exist, rebuild local control without recreating servers:

```sh
# Read-only scan (requires provider API token)
SERVERPRO_SERVER_PROVIDER_TOKEN='...' serverpro server discover -p vultr

# Rewrite local config/state/registry for every managed server on that token
SERVERPRO_SERVER_PROVIDER_TOKEN='...' \
  SERVERPRO_TAILSCALE_TOKEN='...' \
  serverpro server import --all -p vultr --with-tailscale -y

# Optional dry-run plan only
serverpro server import --all -p vultr --dry-run
```

Import claims only resources stamped with `managed-by=serverpro` ownership
labels. It also restores the typed managed access-policy reference needed for
safe cleanup. A provider-only import keeps mandatory Tailscale access enabled
and stores supplied credentials as an incomplete server-scoped set; doctor can
prompt for the missing Tailscale token. SSH additionally needs discovered mesh
host state, so rerun the command with `--force --with-tailscale` before SSH when
the initial import omitted enrichment. Forced import repairs the invalid
disabled-Tailscale config written by earlier releases and preserves existing
service tokens when replacements are omitted. Vultr supplies its attached
firewall-group ID; Hetzner and
DigitalOcean require one exact owned `<server-name>-deny-public` firewall.
Its canonical target is one namespace/server-derived tag; a complete historical
ownership-tag selector set is accepted only when full live Droplet inventory
proves it matches no unrelated resource. Missing, ambiguous, incomplete, or
unsafe policy matches stop discovery/import instead of writing partial
ownership state. Import stages config, credentials, state, and registry
behind a non-secret recovery marker. If a local write fails, rerun the same
import without `--force`; a matching marker resumes the same artifact baseline
and is removed after registry publication. For existing state, `--force` starts
from the valid local
config and state, refreshes provider and explicitly enriched identities, and
preserves operator configuration, Tailscale policy evidence, ingress state, and
same-tunnel ownership provenance. Malformed, unreadable, or concurrently changed
existing artifacts stop the import unchanged. Discover reports a state file
missing its registry entry as `local_state: "partial"`. Sudo passwords are not
recoverable; re-enter
them for doctor/bootstrap when needed.

Status includes provider, catalog shape, power, public IP, Tailscale readiness,
SSH readiness, and ingress summary. Doctor reuses one compute, Tailscale, and
Cloudflare snapshot per run and sends read-only host checks in one remote batch.
That baseline first checks the actual managed-host OS, codename, and architecture,
then checks exact managed pins, the Tailscale client and running daemon, and
updates visible in the existing apt cache; it does not refresh package metadata.
A platform failure disables every requested `--fix` action. If sudo authentication requires a prompt, only remote inventory/checks
rerun; provider and local results remain from the original report.
`serverpro server doctor NAME --fix` refreshes package repositories, upgrades
serverpro-managed apt packages, repairs exact pins, and checksum-verifies a
stale Tailscale release. Tailscale daemon restart is delayed until the updating
SSH command returns, then doctor waits and rechecks it. When ingress is enabled, doctor verifies cloudflared is active and at or above
its reviewed `2026.8.2` package floor. Cloudflared remains conditional ingress
infrastructure and is not upgraded by generic host-tool repair. Bootstrap reruns managed host-tool setup through Tailscale SSH with
password-required sudo. The default `all` target
installs the full managed toolset; run `serverpro server bootstrap --help` for
the canonical list and pinned versions. Focused targets stay `git`, `docker`,
`mise`, `node`, and `pi`; `git` converges Git/OpenSSH plus target-user mise and
gh before offering interactive full account-key or read-only deploy-key access.
Full account-key access requires a masked PAT and stores gh credentials only on
the managed host. Pi authentication remains operator-owned; inspect also
exposes optional hosted/API and model authentication that serverpro does not
configure. Pi is an AI coding agent with arbitrary shell-execution
capability: installing it on a hardened host widens the admin-user trust
boundary, so enable the `pi` or `all` target only where that is intended.
Pi is installed via `npm install -g` under the mise-managed Node; npm
dependencies resolve at install time and are not checksum-vendored, which is a
residual supply-chain risk to weigh before enabling the `pi` target. The `all`
target also installs Node.js `24.20.0` LTS with bundled npm `11.19.0`, Pi
`0.84.3`, uv `0.12.6` through mise's explicit `aqua:astral-sh/uv` backend, and
Rust `1.98.0` through `core:rust` with the default rustc, Cargo, rustfmt, Clippy,
and docs profile. Doctor checks those exact versions and all Rust
default-profile components. Wrong Node/npm state forces same-version Node
replacement; `pi` and `all` reinstall Pi afterward because its global npm
package belongs to that Node installation. It also installs tmux `3.7c`, gh `2.98.0`, rg
`15.2.0`, fd `10.5.0`, ast-grep `0.45.2`, sem `0.23.1`, and inspect `0.1.1`.
ast-grep, sem, and inspect use checksum-pinned GitHub release assets; inspect's bare binary
digest is checked before execution. On existing hosts, `all` removes the active
deprecated `sg` mise key before managing `ast-grep`; mise prunes the old install
when unused.
The same target installs Herdr `0.8.2` through mise's explicit GitHub backend,
verifies the installed Linux release
binary against its architecture-specific SHA-256 digest, and runs
`herdr integration install pi` as the admin user; the
Ubuntu 24.04 and x86_64/arm64 restriction from `INSTALLATION.md` is enforced
before any host change. Directly installed apt packages use reviewed minimums:
bootstrap checks candidates before package scripts run, accepts newer signed
security updates, never downgrades them, and doctor reports both below-floor
packages and cached newer candidates. Doctor
requires the exact Herdr version, digest, and current Pi integration. Serverpro
does not use Herdr's self-updater and never starts, stops, or restarts running
Herdr sessions; rerun managed bootstrap for package updates, then restart sessions
only when the operator chooses. See `INSTALLATION.md` for the install-time
toolset summary.

## Optional ingress

No public app ingress is created by default. Current Cloudflare Tunnel ingress
commands record pending local route metadata only; they do not yet create or
delete Cloudflare routes.

```sh
serverpro server ingress add webapp \
  -n mynamespace \
  --type cloudflare-tunnel \
  --hostname app.example.com
serverpro server ingress list webapp -n mynamespace
serverpro server status webapp -n mynamespace
serverpro server ingress remove webapp \
  -n mynamespace \
  --hostname app.example.com
```

Cloudflare Tunnel ingress is independent from compute providers and does not
require inbound provider firewall openings. Routes shown with `pending` status
are not publicly routable yet.

## Tailnet DNS ownership

Managed servers rely on Tailscale DNS (default `--accept-dns` behavior): after
provisioning, the host resolver is quad100 (`100.100.100.100`), which needs a
usable upstream for public names. The tailnet MUST configure at least two
global nameservers (admin console → DNS) with **Override DNS servers** enabled;
without Override, the control plane does not distribute them and quad100 falls
back to reading host system DNS, which can fail silently and kills all public
name resolution (observed with tailscaled 1.98.10). `serverpro server doctor`
covers this with the provider `tailnet dns` check (MagicDNS enabled but zero
global nameservers warns) and the remote `dns resolution` canary, which
separates resolver failure from egress failure.

## Reconcile tailnet-global policy

Per-server deletion never edits shared Tailscale ACL policy. Remove unused
serverpro policy entries only with the explicit tailnet-wide command:

```sh
# Preview only
SERVERPRO_TAILSCALE_TOKEN='...' \
  serverpro tailnet reconcile example.ts.net --dry-run --non-interactive

# Apply without an interactive confirmation
SERVERPRO_TAILSCALE_TOKEN='...' \
  serverpro tailnet reconcile example.ts.net --yes --non-interactive
```

Interactive runs prompt for the token when neither `SERVERPRO_TAILSCALE_TOKEN`
nor legacy fallback `TAILSCALE_API_TOKEN` is set. The TAILNET argument must name a stable
identity; the token-relative `-` alias is rejected. Registered state carrying
policy evidence must also contain an explicit identity. Existing token-relative
state can be migrated by setting the explicit tailnet in config and rerunning
create, or by rerunning import with `--tailscale-tailnet`.

Reconciliation waits for create/import activity on the same tailnet and fails
closed if registered state is unreadable or its evidence has unresolved
identity. It protects tags only from matching-tailnet state plus all live target
tailnet devices, including unregistered devices. Exact serverpro tag-owner
entries are removed only when no retained tag definition references them as an
owner; recognized mixed SSH rules retain every non-stale destination.
Interactive preview is written to stderr, while stdout contains one
final JSON document. Apply refetches current evidence and policy and aborts when
the removal plan differs from the approved preview; an unchanged plan is
validated and conditionally published with its ETag.

## Delete

Destructive commands always preview the full plan and request approval before
making changes, unless `-y/--yes` is provided. `--dry-run` is also available to
preview and exit immediately.

```sh
# Interactive: plan printed, then confirmation prompt
serverpro server delete webapp -n mynamespace -p hetzner

# Skip preview + approval
serverpro server delete webapp -n mynamespace -p hetzner --yes

# Preview only, no changes
serverpro server delete webapp -n mynamespace -p hetzner --dry-run
```

After approval, serverpro reloads and checks local destructive authority. It
then reads each tracked Tailscale device, Tailscale auth key, and owned
Cloudflare tunnel before compute deletion. An API or ownership error stops the
command before compute mutation. External cleanup repeats each ownership check
immediately before its DELETE.

Independent provider APIs cannot form one atomic transaction. If compute
deletion completes but external cleanup fails, the command exits nonzero and
writes a `partial` JSON result. The result sets `compute_deleted`,
`local_state_retained`, and `retryable`. It also reports `failure_stage`,
`error`, `next_action`, and `remaining_external_cleanup`. Remaining cleanup is
tracked recovery evidence. It does not prove that each resource is still live.
Resolve the reported error, then rerun the same delete command. Compute adapters
treat missing tracked compute resources as already deleted.

Canonical local server artifacts are removed only after provider deletion succeeds.
The dry-run plan lists them in `local_cleanup`: config, credentials, state,
import marker, and adjacent server lock files. The empty canonical credential
directory is also removed. Custom `--config` and `--state` paths and their locks are preserved because
serverpro does not own those locations. The registry entry is removed last;
a local cleanup failure retains it for diagnosis and recovery. Provider resources are
validated before the first provider DELETE. For DigitalOcean servers created
before dedicated per-server firewall tags, cleanup accepts only the exact
historical ownership selectors and first proves through live Droplet inventory
that no unrelated resource matches them. Any ambiguous impact retains local
recovery state. Server delete removes only tracked server-scoped resources;
Cloudflare cleanup is limited to tunnels with `created` provenance. Adopted,
imported, and legacy unknown-provenance tunnels remain untouched. Server delete
never edits tailnet-global ACL policy. Non-interactive mode disables the
approval prompt, so live delete requires
`--yes`.

### Delete a namespace

`namespace delete` removes every managed server in the namespace, tracked
external resources, and all local namespace files. It follows the same
preview-then-approve behavior:

```sh
# Interactive: plan printed, then confirmation prompt
serverpro namespace delete mynamespace

# Skip preview + approval
serverpro namespace delete mynamespace --yes

# Preview only, no changes
serverpro namespace delete mynamespace --dry-run
```

If canonical config, credentials, state, or partial-import markers exist without
matching registry authority, namespace deletion fails before any mutation. If
any server deletion fails, the command stops and leaves the namespace and its
remaining servers intact so the operation can be retried.

## Local files

```text
~/.config/serverpro/namespaces/<namespace>/servers/<server>.yaml
~/.config/serverpro/namespaces/<namespace>/servers/<server>/credentials.json
~/.local/state/serverpro/registry.json
~/.local/state/serverpro/namespaces/<namespace>/servers/<server>.json
```

State is provider-neutral. Managed access policies are stored as typed
`compute.managed_resources` references. Legacy provider-state policy keys are
migrated on read; opaque adapter state remains only for adapter compatibility.
