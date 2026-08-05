# Usage

serverpro manages hardened Ubuntu servers through provider adapters. The CLI is
resource-first and provider-neutral. A namespace is the top-level serverpro
resource group; server credentials are scoped to one server inside that
namespace.

## Global flags

```text
-n, --namespace <id>     serverpro namespace
-p, --provider <name>    compute provider, e.g. hetzner, vultr, or digitalocean
-A, --all                show every matching resource
--dry-run                preview without mutation
--non-interactive        disable prompts; fail when required input is absent
-y, --yes                approve live/destructive commands
--timeout <duration>     operation timeout
```

Command output is JSON on stdout. Prompts and confirmations go to stderr.

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

serverpro server create NAME
serverpro server bootstrap NAME [all|git|docker|mise|node|pi]
serverpro server list
serverpro server status NAME
serverpro server doctor NAME
serverpro server ssh NAME
serverpro server discover
serverpro server import [NAME]
serverpro server start|stop|restart NAME
serverpro server delete NAME

serverpro provider list
serverpro provider status NAME
serverpro provider doctor NAME

serverpro catalog locations -p PROVIDER
serverpro catalog sizes -p PROVIDER --location LOCATION
serverpro catalog images -p PROVIDER --location LOCATION

serverpro ingress list SERVER
serverpro ingress add SERVER --type cloudflare-tunnel --hostname HOSTNAME
serverpro ingress remove SERVER --hostname HOSTNAME
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
and DigitalOcean. Hetzner stores these as labels; Vultr and DigitalOcean store
them as tags:

```text
managed-by=serverpro
serverpro-namespace=<namespace>
serverpro-server=<server>
```

For flat tag providers, values with provider-invalid characters are encoded
reversibly before submission and decoded for ownership checks.

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
`serverpro catalog ...` first to select provider-supported values. Image
catalogs expose only Ubuntu 24.04 LTS because bootstrap and hardening target
that release. Create prompts for missing server-scoped secrets and stores them
at:

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

`catalog` and `provider doctor` run before a server credential file may exist,
so they use an ephemeral server provider token. Interactive runs prompt for it;
non-interactive runs read `SERVERPRO_SERVER_PROVIDER_TOKEN`.

```sh
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro catalog locations -p hetzner --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro catalog locations -p vultr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro catalog locations -p digitalocean --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro provider doctor hetzner --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro provider doctor vultr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro provider doctor digitalocean --non-interactive
```

The Vultr adapter uses numeric OS IDs for `--image` (for example, the value
shown by `serverpro catalog images -p vultr`). The DigitalOcean adapter uses
image slugs for `--image` (for example, `ubuntu-24-04-x64`). The token is used
for that command only and is never stored globally.

### Creating a Vultr server

```sh
# 1. Get your Vultr API token from https://my.vultr.com/settings/#settingsapi
# 2. Check available locations/plans/images
SERVERPRO_SERVER_PROVIDER_TOKEN='vultr-api-token' \
  serverpro catalog locations -p vultr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='vultr-api-token' \
  serverpro catalog sizes -p vultr --location ewr --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='vultr-api-token' \
  serverpro catalog images -p vultr --location ewr --non-interactive

# 3. Preview a create
serverpro server create webapp \
  -n mynamespace -p vultr \
  --location ewr \
  --size vc2-1c-1gb \
  --image 2284 \
  --dry-run

# 4. Run live create, which prompts for Tailscale/Cloudflare secrets
serverpro server create webapp \
  -n mynamespace -p vultr \
  --location ewr \
  --size vc2-1c-1gb \
  --image 2284

# Or non-interactive with env secrets
MYNAMESPACE_WEBAPP_SUDOPASS='use-a-long-remote-admin-password' \
  serverpro server create webapp \
  -n mynamespace -p vultr \
  --location ewr \
  --size vc2-1c-1gb \
  --image 2284 \
  --tailscale-tailnet mytailnet.ts.net \
  --tailscale-tags tag:serverpro-mynamespace \
  --non-interactive --yes
```

Only IPv4-backed instances are created (`enable_ipv6=false`). A Vultr firewall
group keeps public services closed while allowing Tailscale direct connections
on inbound UDP `41641`; STUN on UDP `3478` remains outbound. If creation retries
from a checkpointed group, serverpro validates its ownership, removes only
exact legacy serverpro inbound UDP `3478` rules, and restores missing required
UDP `41641` rules before creating the instance. Ownership uses the shared
provider metadata convention.

### Creating a DigitalOcean server

```sh
# 1. Get your DigitalOcean API token from https://cloud.digitalocean.com/account/api/tokens
# 2. Check available regions/sizes/images
SERVERPRO_SERVER_PROVIDER_TOKEN='digitalocean-api-token' \
  serverpro catalog locations -p digitalocean --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='digitalocean-api-token' \
  serverpro catalog sizes -p digitalocean --location nyc3 --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='digitalocean-api-token' \
  serverpro catalog images -p digitalocean --location nyc3 --non-interactive

# 3. Preview a create
serverpro server create webapp \
  -n mynamespace -p digitalocean \
  --location nyc3 \
  --size s-1vcpu-1gb \
  --image ubuntu-24-04-x64 \
  --dry-run

# 4. Run live create, which prompts for Tailscale/Cloudflare secrets
serverpro server create webapp \
  -n mynamespace -p digitalocean \
  --location nyc3 \
  --size s-1vcpu-1gb \
  --image ubuntu-24-04-x64
```

Only IPv4-backed droplets are created (`ipv6=false`). DigitalOcean tag
resources are ensured first, then a tag-bound firewall is created before the
droplet. It keeps SSH closed, allows Tailscale's direct-connection UDP port
`41641` inbound, and allows outbound traffic, including STUN on UDP `3478`.
A retry from a checkpointed firewall validates ownership and removes only the
exact legacy serverpro broad inbound UDP `3478` rule before creating the
droplet. Ownership uses the shared provider metadata convention. The token
needs tag read/create scope in addition to droplet and firewall scopes.

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
labels. It fails closed if exact region/size/image metadata or one unique owned
provider firewall cannot be recovered. `--with-tailscale` also records the
configured tailnet selector and canonical tailnet ID before attaching a device.
Sudo passwords are not recoverable; re-enter them for doctor/bootstrap when
needed.

Status includes provider, catalog shape, power, public IP, Tailscale readiness,
SSH readiness, and ingress summary. Bootstrap reruns managed host-tool setup
through Tailscale SSH with password-required sudo. The default `all` target
installs the full managed toolset; run `serverpro server bootstrap --help` for
the canonical list and pinned versions. Focused targets stay `git`, `docker`,
`mise`, `node`, and `pi`; `git` also offers the read-only GitHub deploy-key
flow when interactive. Among the managed tools, only Pi and gh carry
credentials; serverpro installs them but never authenticates them or stores
their credentials. Pi is an AI coding agent with arbitrary shell-execution
capability: installing it on a hardened host widens the admin-user trust
boundary, so enable the `pi` or `all` target only where that is intended.
Pi is installed via `npm install -g` under the mise-managed Node; npm
dependencies resolve at install time and are not checksum-vendored, which is a
residual supply-chain risk to weigh before enabling the `pi` target. The `all`
target also installs exact-version Herdr through mise's explicit GitHub backend,
verifies the installed Linux release binary against its architecture-specific
SHA-256 digest, and runs `herdr integration install pi` as the admin user; the
x86_64/arm64-only restriction from `INSTALLATION.md` is enforced before any host
change. Doctor
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
serverpro ingress add webapp \
  -n mynamespace \
  --type cloudflare-tunnel \
  --hostname app.example.com
serverpro ingress list webapp -n mynamespace
serverpro server status webapp -n mynamespace
serverpro ingress remove webapp \
  -n mynamespace \
  --hostname app.example.com
```

Cloudflare Tunnel ingress is independent from compute providers and does not
require inbound provider firewall openings. Routes shown with `pending` status
are not publicly routable yet.

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

State is removed only after all tracked provider and external cleanup succeeds.
Tailscale cleanup uses the persisted tailnet selector, verifies its canonical ID
with current credentials before compute deletion, and uses the exact SSH tags
and admin user recorded when the rule was ensured—not current config. A live
delete preflight reconciles pending policy ownership from an ambiguous write:
full exact presence promotes it, full absence clears it, and partial application
or drift remains blocked for manual repair. Dry-run cannot reconcile because it
makes no provider calls. Before compute deletion, the same preflight validates
every tracked Tailscale and Cloudflare resource and any shared-policy ownership
transfer without provider mutation. Legacy or drifted ownership lacking a
complete identity fails closed instead of guessing. If creation stopped before
any compute resource existed, delete skips that absent resource and cleans
checkpointed external resources. Non-interactive mode disables the approval
prompt, so live delete requires `--yes`.

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

If any server deletion fails, the command stops and leaves the namespace and
its remaining servers intact so the operation can be retried.

## Local files

```text
~/.config/serverpro/namespaces/<namespace>/servers/<server>.yaml
~/.config/serverpro/namespaces/<namespace>/servers/<server>/credentials.json
~/.local/state/serverpro/registry.json
~/.local/state/serverpro/namespaces/<namespace>/servers/<server>.json
```

State is provider-neutral. Provider-specific IDs are stored under generic
`compute` and adapter state fields.
