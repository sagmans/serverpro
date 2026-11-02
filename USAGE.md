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

Doctor and the final doctor phase of `server create` print a JSON summary.
The summary lists only `warn`, `fail`, and `skip` results, plus counts and
`report_path`. Use `--full` on either command for the complete JSON report
on stdout. Both modes save the complete report.

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
serverpro server doctor NAME [--fix] [--full]
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
  openssh: disabled
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
reboot:
  enabled: true
  window: "02:00"
  verify_timeout: 10m
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

`access.openssh: disabled` (the default, also when the key is missing) turns
OpenSSH off on the server, so Tailscale SSH is the only admin path.
`enabled` keeps OpenSSH and skips the `openssh disabled` doctor check. See
OpenSSH off.

`reboot.enabled: true` (the default, also when the section is missing)
installs the gated reboot: it reboots in the daily `reboot.window` (`HH:MM`,
UTC, default `02:00`) only when updates need a reboot and every running
workload is declared to come back. `reboot.verify_timeout` (a duration from
`1m` to `1h`, default `10m`) bounds the check after boot. `false` installs
nothing. See Gated reboot.

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

`serverpro server doctor` also reports `gh token parity` between the stored
`github_pat` and the remote `gh` credential (`~/.config/gh/hosts.yml`) using
truncated SHA-256 fingerprints only. Different fingerprints are a warning,
never an automatic overwrite: either copy can be the stale one. The verdict
comes from GitHub itself, not from the remote `gh` toolchain, so a missing or
unrepaired tool never reads as a dead credential. Only a token GitHub rejects,
or a host whose hosts.yml holds none, is replaced: `doctor --fix` then
redeploys the local PAT before the managed tool apply, rechecks the result,
and reports that both copies are dead (rotate the PAT) when the deployed copy
is rejected as well.

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
create verifies that exact image, and that the location offers the selected
size, against the current selected-location catalog before the first provider
mutation. A rerun that resumes a server already recorded in state skips the
size check, since it orders nothing new. During live create, every externally visible policy, tunnel, auth-key, compute, and device
mutation is checkpointed. A failed create reports its lifecycle phase and known
non-secret resource IDs; rerun create to resume from durable checkpoints or use
`server delete` to clean tracked resources, including access policies recorded
before a compute server ID exists. A retry refetches checkpointed provider access
policy and fails before compute mutation if ownership, rules/selectors, or
attachments broadened. If a tunnel was created before its checkpoint was
published, rerun adopts the one exact-name tunnel instead of creating a
duplicate; multiple exact matches fail as ambiguous. Create also refuses to
continue when more than one tailnet device with the server's name and tags
enrolled after its bootstrap key, or when a rerun finds the recorded device
missing or renamed; the error lists the device IDs. Older devices with the same
name are ignored. Remove the unexpected devices in the Tailscale admin console,
then rerun create. If the recorded device was replaced on purpose (deleted and
re-enrolled with a new node ID), rebind state with
`serverpro server import --force --with-tailscale`. Doctor reports the same conflicts on the `tailscale node`
check with code `tailscale_device_identity` and skips all remote checks and
repairs, including `--fix`, until the conflict is resolved. A definitive checkpoint
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
  serverpro size list -p digitalocean --location fra1 --non-interactive
SERVERPRO_SERVER_PROVIDER_TOKEN='digitalocean-api-token' \
  serverpro image list -p digitalocean --location fra1 --non-interactive

# 3. Preview a create
serverpro server create webapp \
  -n mynamespace -p digitalocean \
  --location fra1 \
  --size s-1vcpu-1gb-amd \
  --image ubuntu-24-04-x64 \
  --dry-run

# 4. Run live create; Cloudflare is requested only when ingress is enabled
serverpro server create webapp \
  -n mynamespace -p digitalocean \
  --location fra1 \
  --size s-1vcpu-1gb-amd \
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

Without `--with-cloudflare`, an interactive import asks whether the servers
use a Cloudflare Tunnel; on yes it asks for the account ID and token. Runs with
`--non-interactive`, `--yes`, or `--dry-run` never ask, so pass
`--with-cloudflare --cloudflare-account-id ID` there. Import reads the token
only from `SERVERPRO_CLOUDFLARE_TOKEN` or the prompt, never from a generic
`CLOUDFLARE_API_TOKEN`, because import saves it in the server's credential file.
For the same reason, import reads the Tailscale token only from
`SERVERPRO_TAILSCALE_TOKEN` or the prompt, never from `TAILSCALE_API_TOKEN`.

Import claims only resources stamped with `managed-by=serverpro` ownership
labels. It also restores the typed managed access-policy reference needed for
safe cleanup. A provider-only import keeps mandatory Tailscale access enabled
and stores supplied credentials as an incomplete server-scoped set; doctor can
prompt for the missing Tailscale token. SSH additionally needs discovered mesh
host state, so rerun the command with `--force --with-tailscale` before SSH when
the initial import omitted enrichment. When only the hostname matches because
the device lost its tags, import refuses to record an untagged device while
namespace tags are configured: doctor would reject it on every run. Tag the
device in the admin console, then rerun. With `--with-cloudflare`, a matched
tunnel is recorded with provenance `imported` and enabled in connector-only
mode, the only tunnel mode config validation accepts. Forced import repairs the invalid
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
Failed package, tool, and other fixable remote checks include the check's own
command output in `evidence`.
The sshd hardening checks share one `--fix` action. It validates the new sshd
config with `sshd -t` before writing it, restores the previous files if the
live config then fails validation, changes the config only when the serverpro
sshd config differs, and never starts an inactive, socket-activated
`ssh.service`. It fails when `sshd -T` still does not show a hardened value,
for example because a lower-numbered file in `/etc/ssh/sshd_config.d` sets it
first. If its output says `restoring the previous files FAILED` or `they fail
sshd -t too`, keep the current session open and fix the named files until
`sshd -t` passes. After it runs, the remaining sshd checks, `sshd units`, and
`listening ports` read the host again instead of reusing the start-of-run
baseline, and a check it did not settle fails without running it again. The `sshd units` check reports an `ssh.socket` or `ssh.service` that
is failed, or enabled but not running; `--fix` clears the failure and starts
the unit the host enables. Masked units pass: with OpenSSH off, both units are
masked, and `--fix` never starts them. The sshd hardening checks still check
the sshd config while OpenSSH is off.
Doctor redacts known credentials before it limits failure evidence to 4 KiB.
Long failures retain the beginning and end, separated by `... [truncated] ...`,
so terminal errors remain visible after bootstrap progress. Successful checks
remain in the saved report, not the default result list.

Reports use the system temporary directory (`$TMPDIR` when set, otherwise
the platform default, usually `/tmp`). The path is
`<temp>/serverpro-<uid>/doctor/<namespace>/<server>/doctor-<unique-id>.json`.
Directories are private (`0700`); report files are `0600`. Each server keeps
its newest 5 reports when cleanup succeeds. The operating system can remove
temporary files, so copy a report elsewhere if you need to keep it.
The complete report includes inventory and all check results, with the same
redaction and evidence limits as stdout. It is not an unbounded command transcript.

The summary status is `fail` if any check fails, otherwise `warn` if any check
warns, otherwise `pass`. Skipped checks do not change the exit status.
If report storage fails, stdout contains the complete report instead. A warning
on stderr explains the storage failure. Doctor failure exit behavior is unchanged.
The existing cloud-init detail log remains at
`~/.local/share/serverpro/logs/<namespace>/<server>/cloud-init-status-long.txt`.

Doctor's `admin password hash` check reports only the crypt scheme of the
admin's shadow entry. It fails on SHA-crypt (`$6$`, written to cloud-init by
older serverpro releases) and other legacy schemes, and when no password is
set. `--fix` asks for the admin sudo password and re-sets it with plain
`chpasswd`, so the host stores it with its default (yescrypt), and clears any
leftover first-boot sudo grant.

`serverpro server doctor NAME --fix` refreshes package repositories, upgrades
serverpro-managed apt packages, repairs exact pins, and checksum-verifies a
stale Tailscale release. Docker checks also reject altered managed Buildx
artifacts and unrecognized plugin overrides before execution. Repair retains
the reviewed binary when apt is below its floor, then returns to a safe apt
package once available. Operator-owned overrides require operator action;
repair does not delete them. Tailscale daemon restart is delayed until the updating
SSH command returns, then doctor waits and rechecks it. When ingress is enabled, doctor verifies cloudflared is active and at or above
its reviewed `2026.10.6` package floor. A failed `cloudflared` check names one
reason: the package is not installed, its package status, the installed version
below the floor, a missing install receipt, a connector that is "not the build
serverpro installed from https://pkg.cloudflare.com/cloudflared", or the service
state from `systemctl is-active`. The receipt is
`/var/lib/serverpro/cloudflared.receipt`. Hosts provisioned before it existed
report it missing until `--fix` reinstalls the connector once.
Cloudflared remains conditional ingress infrastructure and is not upgraded by
generic host-tool repair. Instead, `--fix` runs a dedicated cloudflared repair.
It upgrades from the Cloudflare apt repository that provisioning trusted, then
enables and restarts the service. A connector from `dpkg -i`, another
repository, or an edited binary is reinstalled from Cloudflare, also when it is
newer. When no version is offered only by Cloudflare, the repair stops with
"no cloudflared version is offered only by https://pkg.cloudflare.com/cloudflared";
remove the other source that offers it. It does not change the tunnel token. It fails
with a "reprovision the server" message when the service unit, token file,
pinned keyring, or exact apt source line is missing or changed.

When ingress is `none`, doctor still audits any cloudflared on the host under
the same `cloudflared` check. Ingress `none` only means serverpro set up no
tunnel; another tool can still install or run a connector. The check and
`--fix` read the host state on each run:

| Host state | Check | `--fix` |
| --- | --- | --- |
| No installed package and no `cloudflared` on `PATH` | Pass: `cloudflared not installed` | Nothing |
| `cloudflared.service` enabled or active | Same as with ingress enabled | Same repair as with ingress enabled; needs the unit and token |
| Package present, service neither enabled nor active (dormant) | Floor and install receipt only; prints `cloudflared <version> dormant` | Reinstalls from the trusted Cloudflare repository and writes the receipt; needs no token and never enables, starts, or restarts the service |

The check and `--fix` stop without changes for a package in any other dpkg
state (for example `half-installed`), a `cloudflared` on `PATH` that is not
`/usr/bin/cloudflared`, a unit state other than enabled, disabled, masked, or
not found, and a dormant package while a `cloudflared` process runs outside
the service. The repair trusts only the keyring and apt source line that
provisioning writes, so a connector installed from another key or source
fails with the "reprovision the server" message. The one exception is the
exact line older provisioning wrote, with suite `any` in place of `noble`.
It names the same keyring, repository, and component, so both repairs
rewrite it to the `noble` line and continue.
Provisioning, bootstrap, and `--fix` apt work waits up to 5 minutes for each
apt lock that a concurrent apt run holds, such as apt-daily refreshing package
lists or unattended upgrades installing packages, and prints a
`waiting up to 300 seconds` notice on stderr. If the package lists stay locked
past that budget, the step fails with apt's lock error and
`apt package lists stayed locked for 300 seconds; another apt run still holds them`;
rerun once the other apt run finishes. Repository and other apt errors fail at
once with apt's own output. Bootstrap reruns managed host-tool setup through Tailscale SSH with
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
Pi is installed via `npm install -g` under the mise-managed Node. Serverpro
repairs only that local installation to reviewed `brace-expansion@5.0.12`
and `undici@8.12.3`
with lifecycle scripts disabled. Installation and repair enforce npm’s seven-day
minimum age. Bootstrap requires a clean runtime dependency audit; doctor also
rejects incorrect dependency versions or shrinkwrap integrity. No upstream
release changes. Other dependencies are not fully vendored, so residual npm
supply-chain risk remains. The `all`
target also installs Node.js `24.22.1` LTS with bundled npm `11.19.1`, Pi
`0.87.1`, uv `0.13.5` through mise's explicit `aqua:astral-sh/uv` backend, and
Rust `1.99.1` through `core:rust` with the default rustc, Cargo, rustfmt, Clippy,
and docs profile. Doctor checks those exact versions and all Rust
default-profile components. Wrong Node/npm state forces same-version Node
replacement; `pi` and `all` reinstall Pi afterward because its global npm
package belongs to that Node installation. It also installs tmux `3.7c`, gh `2.103.3`, rg
`15.2.0`, fd `10.5.0`, ast-grep `0.51.0`, sem `0.29.0`, and inspect `0.1.1`.
ast-grep, sem, and inspect use checksum-pinned GitHub release assets; inspect's bare binary
digest is checked before execution. On existing hosts, `all` removes the active
deprecated `sg` mise key before managing `ast-grep`; mise prunes the old install
when unused.
The same target installs Herdr `0.9.5` through mise's explicit GitHub backend,
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

### Drift checks

These doctor checks compare the live host with what config and provisioning
set up. Most are read-only: `--fix` does not change what they find, because
each finding needs an operator decision. Six checks have a repair, because
config, provisioning, or the compute provider already states the wanted
result: `egress restricted` re-applies the restricted egress lockdown,
`openssh disabled` turns OpenSSH off after its safety checks,
`reboot survivability` installs the gated reboot's script and units,
`unattended upgrades config` rewrites the serverpro unattended-upgrades
drop-in, `privileged accounts` removes the provider's own image accounts, and
`cloudflared apt pin` rewrites the pin (only with ingress
`cloudflare-tunnel`).

| Check | Result | Finding |
| --- | --- | --- |
| `egress restricted` | fail | UFW's default outgoing policy is not `deny`/`reject`, a lockdown `ALLOW OUT` rule is missing, or a TCP connect to `1.1.1.1:853` succeeds. Runs only when the lockdown is due: `network.egress.mode: restricted` and `phase_lockdown_after_bootstrap` not `false`, the same rule create and `server bootstrap` use; `--fix` re-applies the lockdown. |
| `openssh disabled` | fail or warn | `ssh.socket` or `ssh.service` is enabled or active, or a process listens on TCP port 22, or `sshd` listens on any port (fail). A unit is disabled but not masked, or `/etc/ssh/sshd_not_to_be_run` is missing (warn). Skipped with `access.openssh: enabled`; `--fix` runs the create step, safety checks included, and reads the tunnel's ingress at Cloudflare right before it; a route to SSH or a failed read keeps the repair from running (`fix not run`). See OpenSSH off. |
| `reboot pending` | warn | `/var/run/reboot-required` exists, or an installed kernel is newer than the running one. Doctor never reboots, also with `--fix`. With `reboot.enabled: true` the gated reboot runs in the window once `reboot survivability` shows no gap; see Gated reboot. |
| `reboot survivability` | fail or warn | Fail: with `reboot.enabled: true`, the gated reboot's script, timer, or units are missing, changed, or not enabled (`install:` findings). Warn: a running workload would not come back after any reboot (see Gated reboot for the rules), the post-reboot check in the current boot was not `ok` or did not finish, or the timer is enabled while `reboot.enabled: false`. `--fix` repairs only the `install:` findings: it never reboots and never changes a workload. |
| `unattended upgrades config` | fail or warn | An option of `/etc/apt/apt.conf.d/52serverpro-unattended-upgrades` has another effective value in `apt-config` (fail), or is not set at all (warn: a host created before the drop-in). `--fix` rewrites the drop-in; a later apt.conf.d file that overrides it stays a finding. |
| `privileged accounts` | fail or warn | Doctor reads every rule sudo reads with sudo's own parser (`/usr/bin/cvtsudoers` from the `sudo` package, JSON with aliases expanded, every `@include`, `#include`, and includedir followed); a missing parser, a parse failure, or output doctor cannot read fails the check. Fail too when sudo may read rules the parser does not, or load or run a swapped helper as root: `/etc/sudo.conf` loads a plugin other than `sudoers.so`, passes `sudoers.so` an option other than `error_recovery` (such as `sudoers_file`, `sudoers_uid`, `sudoers_gid`, or `ignore_perms`), sets `Path plugin_dir`, `noexec`, `intercept`, or `sesh` to other than the packaged `/usr/libexec/sudo`, `/usr/libexec/sudo/sudo_noexec.so`, `/usr/libexec/sudo/sudo_intercept.so`, or `/usr/libexec/sudo/sesh`, or sets any other `Path` name but `askpass`, or the first `sudoers:` line in `/etc/nsswitch.conf` names a source other than `files`; the finding names the file and line. Fail: an account other than the admin user is in `sudo`, `admin`, `lxd`, or `docker` (also as primary group), a rule grants sudo to another account, `#uid`, or group without a command limit (`ALL`, a relative or wildcard path, or a directory), a non-root account has uid 0, or a provider image account (Vultr: `linuxuser`) exists. Fail too: such a rule lists a root-equivalent command (a shell, editor, pager, interpreter, file writer, loader, or account, package, service, module, capability, or container manager such as `bash`, `vim`, `python3`, `tee`, `sort`, `systemctl`, `modprobe`, `setcap`, `ld-linux-*`, or `docker`), matched by name with any version suffix or GNU target prefix dropped and whatever its arguments; it has `SETENV` or an option doctor does not grade as safe (such as `CWD=` or `CHROOT=`); or a `Defaults` entry for any binding sets an option or value doctor's allowlist does not pass. The allowlist names each graded option with the values it may hold (Ubuntu's stock `env_reset`, `mail_badpass`, `secure_path`, and `use_pty`, serverpro's `verifypw=any`, and the logging, mail, prompt, and timeout options), so `env_file`, `mailerpath`, `runchroot`, `!authenticate`, `!env_reset`, `setenv`, and `!secure_path` fail. `env_keep` and `env_check` may add only `COLORS`, `DISPLAY`, `HOSTNAME`, `KRB5CCNAME`, `LS_COLORS`, `PS1`, `PS2`, `XAUTHORITY`, `XAUTHORIZATION`, `XDG_CURRENT_DESKTOP`, `COLORTERM`, `LANG`, `LANGUAGE`, `LC_*`, `LINGUAS`, `TERM`, and `TZ`, the names sudo keeps or checks by default less `PATH`. A `secure_path` directory, `logfile`, or `iolog_dir` that is relative, holds a `%` escape, or fails the path walk below fails, and so does a host where no unbound `Defaults` sets `secure_path`. Fail too, on the server: a hop of a listed path (each symlink, up to 40) or the directory it sits in is not root-owned, can be written by group or others, or has an ACL; the last hop is not a regular file or is a root-equivalent command; or a file with the listed name plus a space exists, so an escaped space cannot hide a different command. A `Defaults` path gets the same walk and must end at a directory (`secure_path`, `iolog_dir`) or a regular file (`logfile`), or not exist yet. Warn: such a rule lists only other absolute command paths, directly or through a `Cmnd_Alias`; the finding names them. `--fix` removes only the provider image accounts; see Provider image accounts. |
| `metadata user-data` | fail or warn | The provider metadata user-data holds the admin's current password hash (fail), or another password hash or `passwd` key (warn). |
| `tailscale operator` | fail | tailscaled has an `OperatorUser`. |
| `tailscale serve` | fail or warn | Funnel publishes a port (fail), or a Serve entry forwards to a local port with no TCP listener (warn). The evidence shows a proxy target as scheme, host, port, and path only, never its userinfo, query, or fragment. A failed read reports the command that failed and its exit status, never the Serve config. |
| `sysctl runtime` | warn | A running kernel value differs from `/etc/sysctl.d/99-serverpro.conf`, or the file lacks or changed a serverpro value. |
| `cloudflared ingress` | fail | Config ingress is `none`, but `cloudflared.service` is enabled or running, or the host state cannot be classified (for example a `cloudflared` process outside the service). A dormant package passes. Runs only with ingress `none`. |
| `cloudflared apt pin` | fail | The serverpro apt pin for cloudflared is missing or changed. Runs only with ingress `cloudflare-tunnel`; `--fix` rewrites only the pin. |
| `tailnet policy` (provider) | warn | The tailnet policy lets every source reach every port. See `docs/tailscale-setup.md`, Recommended grants policy. |

A probe that cannot read the host fails with "the read-only probe could not
run". Drift is then unknown, so doctor does not report a pass. The
`metadata user-data` check reports presence only: doctor never prints the
user-data, the hash, or the shadow entry. The compute provider adapter supplies
the metadata path; with no path, the check is skipped. With ingress `none`,
the `cloudflared` check above audits and repairs the connector itself;
`cloudflared ingress` only reports that a live connector contradicts config,
and it reads the same host state as that audit. The `tailnet policy` check
needs an API token that can read the policy file; serverpro never edits
tailnet packet rules.

### OpenSSH off

With `access.openssh: disabled` (the default), `server create` turns OpenSSH
off as its last remote step, after the bootstrap reached the server through
Tailscale SSH. `serverpro server doctor NAME --fix` does the same on an older
server, and after a create that stopped before this step: `server bootstrap`
does not run it. Doctor never does this without `--fix`, and it never
reboots.

The step first checks these conditions. If one of them is true, it stops and
changes nothing:

- `tailscaled` is not active.
- `tailscale debug prefs` does not show `"RunSSH": true`.
- A TCP connection to port 22 is established.
- The journal (`sshd`, `sshd-session`) or `/var/log/auth.log*` shows an
  OpenSSH login in the last 30 days. The message gives only the method and
  the user, never the source address. An `auth.log` line in the classic
  syslog format has no year, so it counts even when it is older.
- A cloudflared config on the server may reach SSH, or cannot be read.
  Each `service:` and `url:` value must pass the same allowlist as the
  tunnel's remote ingress (see below), and a `bastionMode`, a `proxyType`,
  or a `bastion` or `socks5` key that is not false stops the step. So does
  `ssh://`, or port 22 with any leading zeros (`:022`), anywhere in the
  file. A value the step cannot read whole on its line (a block scalar, an
  anchor or alias, an escaped or unterminated quote) counts as a route. The
  step reads each config that a `cloudflared` unit names with `--config`,
  and every directory cloudflared searches when no `--config` is given:
  `~/.cloudflared`, `~/.cloudflare-warp`, and `~/cloudflare-warp` under
  every home directory in the account database and every `HOME` a
  `cloudflared` unit sets, then `/etc/cloudflared` and
  `/usr/local/etc/cloudflared`. A `--config` path that is not absolute, or
  that does not exist, also stops the step, and so does a unit `HOME` that
  is not absolute.
- A `cloudflared` unit (`cloudflared.service`, or any running
  `cloudflared*.service`) sets `--url` or `TUNNEL_URL` to an origin the
  allowlist does not pass, or turns on `--bastion`, `TUNNEL_BASTION`,
  `--socks5`, or `TUNNEL_SOCKS`, in its `ExecStart` or `Environment`, in an
  `EnvironmentFile` it reads, or in `/etc/default/cloudflared`. Only `0`,
  `f`, `F`, `false`, `False`, and `FALSE` turn those modes off. These
  settings also hold the tunnel token, so the message names only the kind
  of setting, such as `bastion mode`, and prints no value.
- A `cloudflared` process runs outside the `cloudflared*.service` units,
  for example in a container or from a shell. The step finds it by its
  process name and by the executable it runs (`/proc/PID/exe` named
  `cloudflared*`), so a symlink or a renamed launcher does not hide it. Its
  settings are not on the server to read, so the message gives its process
  ID and cgroup. Make sure it does not route to SSH, stop it, and rerun.
- The server has a Cloudflare tunnel in state, and its ingress at Cloudflare
  has a rule that may reach SSH. The check is an allowlist. A rule passes
  only as `hello_world`, `http_status:NNN`, a `unix:` or `unix+tls:` socket,
  or an `http`, `https`, or `tcp` origin on a port other than 22 (a `tcp`
  origin must name its port), and only without bastion mode or a proxy type.
  The port counts as a number, as cloudflared dials it, so `0022` is port
  22, and a port above 65535 counts as a route.
  Every other rule counts: `ssh://` or `rdp://` origins, the `bastion` and
  `socks-proxy` services, a rule without a service, and any service
  serverpro cannot classify.
  Serverpro creates remotely managed tunnels, so a route added in the
  Cloudflare dashboard never reaches the server's disk. Serverpro reads the
  ingress with the Cloudflare API before it runs anything on the server.
  Origin settings set for the whole configuration count for every rule.
  Serverpro also lists the tunnel's private network and private hostname
  routes, and any one counts as a route, because WARP clients reach every
  port through it, port 22 included. cloudflared serves such routes whatever
  the configuration's `warp-routing` setting says, so that setting does not
  count. The lists need the `Cloudflare Tunnel Read` account permission
  (`Cloudflare Tunnel Edit` includes it); a failed list read stops the step
  and names it. The message names each rule's service with any
  URL password masked, or a fixed label for a service it cannot classify,
  never its hostname. If the read fails, or the answer
  does not name the tunnel, the step stops too: serverpro cannot tell that no
  route needs sshd. A tunnel that has never had a remote configuration, as
  serverpro creates it, has no routes.

Then it:

1. Warns when the admin user has no usable password, or when a provider
   console agent that logs in through sshd is active (DigitalOcean:
   `droplet-agent`). `doctor --fix` shows these warnings in its result.
2. Writes the original `is-enabled` state of both units, and whether the flag
   file existed, to `/var/lib/serverpro/openssh-disabled.state` (`0600`)
   once. A later run keeps the first record.
3. Runs `systemctl disable --now ssh.socket ssh.service`, then
   `systemctl mask ssh.socket ssh.service`.
4. Creates `/etc/ssh/sshd_not_to_be_run`. Both units do not start while it
   exists, also after someone unmasks them or a package upgrade tries to
   start them.
5. Checks that both units are inactive and that nothing listens on port 22.

The `openssh-server` and `openssh-sftp-server` packages stay installed, so
security updates still apply and the rollback needs no download. Tailscale
SSH has its own SFTP support.

To turn OpenSSH on again, set `access.openssh: enabled` in the server config,
then run on the server:

```sh
sudo cat /var/lib/serverpro/openssh-disabled.state
sudo systemctl unmask ssh.socket ssh.service
sudo rm /etc/ssh/sshd_not_to_be_run
sudo systemctl enable --now ssh.socket
```

These commands fit the usual record on Ubuntu 24.04: `ssh.socket=enabled`
and `condition=absent`. Enable each unit that the record lists as `enabled`,
and keep the flag file when it says `condition=present`.

Recovery without Tailscale SSH uses a provider console that does not need
sshd, and you log in there with the admin user's password:

- Vultr: the View Console.
- Hetzner: the Cloud Console.
- DigitalOcean: the Recovery Console. The Droplet Console logs in through
  sshd, so it does not work while OpenSSH is off.

When no sudo user has a password, use the provider's rescue system instead.

### Provider image accounts

Some stock images create a login account next to root (Vultr:
`linuxuser`, with sudo). Serverpro manages one admin user, so `server create`
removes these accounts after the admin password step proves that the admin
user works through sudo. `serverpro server doctor NAME --fix` removes them
from older hosts. First, the removal stops with nothing changed when sudo
may read rules from outside the sudoers files or run a swapped helper as
root: the `/etc/sudo.conf` and `/etc/nsswitch.conf` settings that
`privileged accounts` fails on. The checks below read only the sudoers
files, so they could not then prove that the account holds nothing. For
each account, the removal then:

1. Stops before any change if `/etc/sudoers`, or any other file sudo reads
   that is not directly in `/etc/sudoers.d` (a file an `#include`,
   `@include`, or includedir names, a nested path such as
   `/etc/sudoers.d/sub/x`, or a path with a `.` or `..` part), names the
   account, if a rule names the account by uid (`#1001`), or if `visudo -c`
   already fails. Serverpro never edits those files or uid rules, and a rule
   there would outlive the account. Take the account out with
   `visudo -f FILE` and rerun.
2. Locks the account and expires it, so no new session starts.
3. Stops if the account has running processes. The account stays locked, and
   the message names the command that undoes the lock: it puts back the
   expiry and the shell the account had (an empty shell field included), and
   unlocks the password only if it was not locked before. End the processes,
   for example with `sudo loginctl terminate-user linuxuser`, and rerun.
4. Archives its home and crontab under
   `/root/serverpro-removed-accounts/<account>-<UTC time>/`.
5. Moves each `/etc/sudoers.d` file whose rules are all for that account into
   the same archive. From a file that also has rules for other accounts, it
   removes only that account's rules and keeps the original in the archive.
   It stops before any change when such a file still names the account in
   another place, for example a `User_Alias` or a rule for several accounts
   (`linuxuser , other ALL=...`): edit that file with `visudo -f` and rerun.
   After each change it runs `visudo -c`. If that fails, it puts the original
   back and stops.
6. Reads the rules with sudo's own parser (`cvtsudoers`) and stops, with the
   account locked and the undo command in the message, if any rule or
   `Defaults` entry still names the account or its uid in a form the edits
   above cannot see, such as an escaped character in the name.
7. Runs `userdel -r`. No sudoers rule for the name or uid stays, so an
   account created later with the same name or uid gets no sudo from it.

A read that fails, rather than finding nothing (the account database, the
process list, a sudoers file, or sudo's parser), stops the removal too. So
does parser output that is not in the exact form the check reads, for
example other spacing, a quoted uid, or an escaped name: the check must read
every account name and uid in it, or it cannot tell that none is left.

Any other account that `privileged accounts` lists is report-only, also
with `--fix`, and doctor never suggests removing it. A CI deploy user that
logs in through Tailscale SSH is the usual case: its sudoers.d rule should
list the commands it needs, and then the finding is only a warning that names
them. Check that the list is still what CI needs. A root-equivalent command
such as `/usr/bin/docker`, `systemctl`, or a shell still fails, also behind
a symlink or a Debian alternative such as `editor`: give the account a
root-owned wrapper script that it cannot write and that runs one fixed
action. The wrapper and every directory above it must be root-owned and not
writable by group or others, or the rule fails too. If the rule grants `ALL`, limit it to those commands and run
`visudo -c`.

### Reboots and kernel cleanup

Cloud-init writes `/etc/apt/apt.conf.d/52serverpro-unattended-upgrades` with
`Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";`. Every kernel
update installs a new image, and without this cleanup old images fill
`/boot` until a later kernel update fails. The file sorts after Ubuntu's
`50unattended-upgrades`, so its value wins. Serverpro owns the file: put
local options in a file that sorts later.

The drop-in also sets `Unattended-Upgrade::Automatic-Reboot "false";`.
unattended-upgrades does not check whether workloads come back, so the gated
reboot below owns update reboots.

To reboot before the window, or with `reboot.enabled: false`, run the
preflight first, then reboot:

```sh
serverpro server ssh NAME -n NAMESPACE
sudo serverpro-reboot preflight
sudo systemctl reboot
```

Then rerun doctor. A manual reboot has no post-boot check.
`serverpro server restart` uses the provider's reboot API, which is a hard
reset on some providers, so it is not the recommended path for an update
reboot.

### Gated reboot

With `reboot.enabled: true`, `server create` installs
`/usr/local/sbin/serverpro-reboot` and three units, after the egress lockdown
and before OpenSSH goes off. On an older server, `serverpro server doctor NAME
--fix` installs them. Doctor never installs them without `--fix`.

| Unit | Role |
| --- | --- |
| `serverpro-reboot.timer` | Starts `serverpro-reboot.service` each day at `reboot.window` UTC. A window missed while the host was off is not run later. |
| `serverpro-reboot.service` | Runs only when `/var/run/reboot-required` exists. It runs the preflight. With a gap, it logs each gap and skips the reboot; the next window tries again. With no gap, it writes the snapshot and runs `systemctl reboot`. |
| `serverpro-reboot-verify.service` | Runs once at boot when a snapshot exists, after `docker.service`. It checks that the snapshot comes from the previous boot and is at most one hour old. It then waits up to `reboot.verify_timeout` for each unit, container, and TCP port in the snapshot. It restarts a missing unit or container at most once, never starts a workload that did not run before the reboot, and writes the result. |

The preflight reports each running workload that a reboot would drop:

- A running service that boot does not start again. A service comes back
  when its unit file state is `enabled` or `generated`, an enabled or
  static socket, timer, or path triggers it, or an active target, a device,
  or a running service wants it. A service whose D-Bus name has a system
  bus activation file (`<name>.service` under
  `/usr/local/share/dbus-1/system-services`, `/usr/share/dbus-1/system-services`,
  or `/lib/dbus-1/system-services`) comes back on demand, such as
  `upower.service`, which is `disabled`, but only when the file's
  `SystemdService=` names that service or one of its aliases. The first file
  in that directory order decides, as it does for the bus. A `static` or
  `disabled` unit that none of these starts, such as one started by hand, is
  a gap. The bus rule does not apply to a `masked` unit or to a `transient`
  one, such as a `systemd-run` service, which a reboot removes.
- A running container with a restart policy other than `always` or
  `unless-stopped`. Docker does not start a `no` container when the daemon
  starts. It starts an `on-failure` container only when the shutdown made it
  exit with an error, so a container that stops cleanly stays down. Running
  containers while `docker.service` is not enabled are a gap too.
- Containers that keep running while `docker.service` is stopped.
  Bootstrap turns on Docker's `live-restore`, so a container outlives its
  daemon, but the gate cannot read its restart policy then.
- A listening socket of a process in a login session, a tmux or screen
  session, a `nohup` process, or a `systemd-run` scope.
- A running service in the systemd user manager of a user without linger,
  or a user manager whose services or account the gate cannot read.

The gate sees four kinds of workload: system services, systemd user
services, Docker containers, and processes that hold a listening TCP or UDP
socket. It does not see any other kind, so it neither blocks a reboot for
one nor waits for one after the reboot. Examples are a Podman container
(serverpro does not install Podman), a tmux or screen session, a `nohup`
process, and a cron job's child process, each when it holds no listening
socket. Before you turn on the gated reboot, move such a workload into a
systemd service or a Docker container with a restart policy.

A logged-in user with no listener does not block a reboot. Known limit: the
gate does not check the services of a user with linger. That user's
manager starts at boot, but a user service in it that is not enabled does
not come back. Run the preflight at any time:

```sh
serverpro server ssh NAME -n NAMESPACE
sudo serverpro-reboot preflight
```

Exit status 0 means no gap, and 3 means gaps, one line each with the
operator command. A user manager the gate cannot read is a gap too. Any
other status means the preflight could not read the host; the window then
skips the reboot. Doctor runs the same preflight as
`reboot survivability`. Fix each gap yourself, for example with
`docker update --restart unless-stopped NAME` or
`sudo systemctl enable UNIT`, or move a process out of tmux into a service.
Doctor never changes a workload.

The gate keeps its files in `/var/lib/serverpro/`, mode `0600`, owner root:

- `reboot-snapshot`: unit names, container names with image and health
  status, and listening TCP addresses below the ephemeral port range. It has
  no environment, command lines, or secrets. After the check it becomes
  `reboot-snapshot.last`.
  The snapshot also records the boot ID and the write time.
- `reboot-verify.result`: `status=ok`, `status=degraded`, or `status=stale`,
  with one `finding=` line for each problem. `degraded` means a workload did
  not come back, or the check was stopped before it finished. `stale` means
  the snapshot came from the running boot or was older than one hour, so
  the check compared and started nothing. Doctor warns on a result that is
  not `ok` while the host still runs the boot that was checked.

Doctor also warns when a snapshot exists but neither the window nor the
check runs, and when the check failed in the running boot before it wrote a
result. Both mean the boot was never compared with the snapshot. If
`systemctl reboot` fails, the window removes its snapshot, so a later,
unrelated reboot does not drive a check.

The gate logs to journald with the tag `serverpro-reboot` and the fields
`SERVERPRO_EVENT` and `SERVERPRO_SUBJECT`:

```sh
journalctl -t serverpro-reboot
journalctl SYSLOG_IDENTIFIER=serverpro-reboot SERVERPRO_EVENT=reboot-skipped
```

Events: `window-skip`, `gap`, `reboot-skipped`, `rebooting`, `verify-skip`,
`verify-stale`, `restart`, `restart-result`, `finding`, `verify-ok`,
`verify-degraded`, and `window-error` or `verify-error` when the gate cannot
read the host, `systemctl reboot` fails, or the check is stopped.

With `reboot.enabled: false`, doctor still runs the preflight and warns while
the timer is enabled. Doctor does not remove the gate; turn the timer off
with `sudo systemctl disable --now serverpro-reboot.timer`.

### Restricted egress

With `network.egress.mode: restricted` and
`phase_lockdown_after_bootstrap: true`, `server create` applies this UFW
lockdown as root after the server tools; only the OpenSSH-off step follows
it. `server bootstrap` applies it again
after the tools, because it is the recovery for a create that stopped before
the lockdown. A create that cannot reach the host fails instead of completing
without the lockdown. The lockdown only adds rules, so rules that other tools
added (for example the Docker bridge rules) stay:

```sh
ufw default deny outgoing
ufw allow out 53
ufw allow out 123/udp
ufw allow out 80/tcp
ufw allow out 443/tcp
ufw allow out 22/tcp
ufw allow out 7844/tcp
ufw allow out 7844/udp
ufw allow out 41641/udp
ufw --force reload
```

The lockdown then proves itself and exits non-zero, which fails create,
bootstrap, or the doctor repair, when any of these is true:

- UFW does not report `deny` (or `reject`) for outgoing, for example because
  UFW is inactive.
- A TCP connect to `1.1.1.1:853` succeeds. No lockdown rule allows it.
- A TCP connect to `controlplane.tailscale.com:443` fails after 3 attempts.
  The host would lose its Tailscale coordination server, and with it the
  operator's only SSH path.

When `egress restricted` fails, find out first which workload needs other
outbound ports (`sudo ufw status verbose`, `ss -tnp`). Then run
`serverpro server doctor NAME --fix`. It re-applies the lockdown and reads the
host again. The `ufw ssh egress` check uses the same repair. An
`ALLOW OUT Anywhere` rule also opens egress: delete it with
`sudo ufw status numbered` and `sudo ufw delete N`.

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
separates resolver failure from egress failure. The remote `egress positive`
check passes on any HTTP answer from its targets, because sites may refuse
datacenter addresses with 403 or 429. It fails only when a target gives no
answer, and names that target in the evidence.

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

credentials.json holds service tokens (`server_provider_token`,
`tailscale_token`, `cloudflare_token`) and stored operator auth
(`admin_sudo_password`, `github_pat`, optional `tailscale_auth_key`) as
plaintext `0600`; a stored `tailscale_auth_key` is never used, because
serverpro always mints its own short-lived tagged bootstrap key. The sudo password resolves from in-process cache, then the
`<NAMESPACE>_<SERVER>_SUDOPASS` env var, then credentials.json, then a prompt;
prompted or env values are stored unless `admin.store_console_password: false`.
The GitHub PAT prompted during full development access setup is stored only
after the remote accepted it, then reused on later runs; a rejected PAT is not
recorded, so the next run asks for a working one instead of retrying it. Configs written before schema stamping carry a tool-forced
`store_console_password: false`; loading one migrates it to stored, and any
later config save records `schema_version: 1`. A stamped explicit `false` stays
runtime-only.

State is provider-neutral. Managed access policies are stored as typed
`compute.managed_resources` references. Legacy provider-state policy keys are
migrated on read; opaque adapter state remains only for adapter compatibility.
