# Installation

serverpro is a Go CLI for provisioning hardened Ubuntu servers. Start with a
dry-run, then move to live provider calls only after credentials and recovery
paths are ready.

## Requirements

- Go 1.26.5+
- Git
- `ssh`
- `tailscale` CLI for local SSH checks and `serverpro server ssh`
- Provider API token for Hetzner, Vultr, or DigitalOcean
- Tailscale API token for target tailnet member users, policy, auth keys, and devices
- Optional Cloudflare API token/account for Cloudflare Tunnel connector metadata
- Optional `fzf` for interactive selections

## Install

Latest release:

```sh
go install github.com/sagmans/serverpro/cmd/serverpro@latest
export PATH="$(go env GOPATH)/bin:$PATH"
serverpro --help
```

Specific release:

```sh
go install github.com/sagmans/serverpro/cmd/serverpro@vX.Y.Z
```

If `GOBIN` is set, add that directory to `PATH` instead.

From source:

```sh
git clone https://github.com/sagmans/serverpro.git
cd serverpro
go build -o serverpro ./cmd/serverpro
./serverpro --help
```

## First dry-run

Preview before any live infrastructure work:

```sh
serverpro server create webapp -n mynamespace -p hetzner \
  --location fsn1 --size cx23 --image ubuntu-24.04 --dry-run
```

`server create --dry-run` prints a provisioning plan without provider calls or
local state writes.

## First live create

Run live create only after reading `SECURITY.md` and confirming provider,
Tailscale, and recovery access:

```sh
serverpro namespace create mynamespace
serverpro server create webapp -n mynamespace -p hetzner \
  --location fsn1 --size cx23 --image ubuntu-24.04
```

Create prompts for missing server-scoped credentials and stores them under:

```text
~/.config/serverpro/namespaces/<namespace>/servers/<server>/credentials.json
```

Credential directories are `0700`; files are `0600`.

## Managed server tools

Create and `server bootstrap NAME all` install the default host toolset. Run
`serverpro server bootstrap --help` for the canonical managed tool list and
pinned versions. The default set includes digest-verified Herdr plus its Pi
lifecycle integration. Herdr publishes Linux x86_64 and arm64 binaries only.
The default `all` toolset (and doctor) requires one of those architectures;
bootstrap rejects any other host before making changes. Pi and gh authentication
remain an operator task; serverpro does not configure or store their credentials.
Serverpro updates
Herdr only during managed bootstrap and never starts, stops, or restarts Herdr
sessions. See `USAGE.md` for bootstrap targets and the doctor flow.

## Automation

Use `--non-interactive` only for fully scripted runs where every required
value is already provided by flags, files, or env vars. Local operator flows can
omit it so serverpro prompts for missing values. Use `--yes` to approve live or
destructive operations without a confirmation prompt.

Non-interactive create requires existing server credentials and the target sudo
password env var:

```sh
MYNAMESPACE_WEBAPP_SUDOPASS='use-a-long-remote-admin-password' \
  serverpro server create webapp -n mynamespace -p hetzner \
  --location fsn1 --size cx23 --image ubuntu-24.04 \
  --non-interactive --yes
```

Catalog and provider checks can use an ephemeral provider token:

```sh
SERVERPRO_SERVER_PROVIDER_TOKEN='provider-token' \
  serverpro catalog locations -p hetzner --non-interactive
```

## Upgrade

Do not run different serverpro versions against the same local state tree.
Before replacement, back up `~/.config/serverpro` and
`~/.local/state/serverpro`. Current releases use schema version 1, accept legacy
unversioned files as version 1, and perform no automatic migration. Unknown
explicit schema versions fail without rewriting state. Before `v1.0.0`, same
schema number does not guarantee downgrade compatibility because safety metadata
can grow between releases.

```sh
go install github.com/sagmans/serverpro/cmd/serverpro@latest
serverpro --version
serverpro server list
```

Pre-`v1.0.0` downgrades are unsupported. To roll back, stop the new binary,
restore both config and state backups made before replacement, then reinstall
the prior version. Never run an older binary against state already rewritten by
a newer one.

Earlier pre-release builds may have created broad inbound UDP `3478` rules on
DigitalOcean or Vultr firewalls. New firewalls expose only UDP `41641` for
Tailscale direct connections; STUN on UDP `3478` remains outbound. A create
retry removes an exact legacy serverpro rule after validating checkpointed
firewall ownership. Completed servers are not changed automatically: inspect
the serverpro-owned provider firewall, remove only broad inbound UDP `3478`
rules created by the old build, preserve outbound UDP, and verify inbound UDP
`41641` plus public SSH closure before upgrading.

## Uninstall

Remove the binary. Delete managed servers first when you no longer need the
provider resources:

```sh
serverpro server delete webapp -n mynamespace -p hetzner --dry-run
serverpro server delete webapp -n mynamespace -p hetzner --yes
```
