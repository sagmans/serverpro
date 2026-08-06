# Installation

serverpro is a Go CLI for provisioning hardened Ubuntu servers. Start with a
dry-run, then move to live provider calls only after credentials and recovery
paths are ready.

## Requirements

- Go 1.26+
- Git
- `ssh`
- `tailscale` CLI for local SSH checks and `serverpro server ssh`
- Provider API token for Hetzner, Vultr, or DigitalOcean
- Tailscale API token for target tailnet policy, auth keys, and devices
- Optional Cloudflare API token/account for Cloudflare Tunnel connector metadata
- Optional `fzf` for interactive selections
- GitHub CLI (`gh`) when verifying downloaded release attestations

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

## Verify release downloads

Each target archive has a matching SPDX SBOM, provenance bundle, and SBOM
bundle. Select one of `linux-amd64`, `linux-arm64`, `darwin-amd64`, or
`darwin-arm64`, then download only that target's files:

```sh
version=vX.Y.Z
target=linux-amd64
archive="serverpro-${version}-${target}.tar.gz"
sbom="serverpro-${version}-${target}.spdx.json"
provenance_bundle="serverpro-${version}-${target}.provenance.sigstore.json"
sbom_bundle="serverpro-${version}-${target}.sbom.sigstore.json"

gh release download "${version}" \
  --repo sagmans/serverpro \
  --pattern SHA256SUMS \
  --pattern "${archive}" \
  --pattern "${sbom}" \
  --pattern "${provenance_bundle}" \
  --pattern "${sbom_bundle}"
```

Verify every downloaded asset against its exact checksum entry before using
it. The checksum file detects transfer errors; publisher identity comes from
the attestations verified next.

```sh
grep -F "  ${archive}" SHA256SUMS | shasum -a 256 -c -
grep -F "  ${sbom}" SHA256SUMS | shasum -a 256 -c -
grep -F "  ${provenance_bundle}" SHA256SUMS | shasum -a 256 -c -
grep -F "  ${sbom_bundle}" SHA256SUMS | shasum -a 256 -c -

gh attestation verify "${archive}" \
  --repo sagmans/serverpro \
  --bundle "${provenance_bundle}"
gh attestation verify "${archive}" \
  --repo sagmans/serverpro \
  --bundle "${sbom_bundle}" \
  --predicate-type https://spdx.dev/Document/v2.3
```

The second attestation binds that target archive to its SPDX predicate. A
repeated publication is rejected; existing release assets are never silently
replaced.

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

Create first installs checksum-pinned Tailscale binaries for supported amd64
or arm64 Ubuntu hosts. Create and `serverpro server bootstrap NAME all` install the
default host toolset. Run `serverpro server bootstrap --help` for the
canonical managed tool list and pinned versions. The default set includes uv
`0.12.0` through mise's explicit `aqua:astral-sh/uv` backend and Rust `1.97.1`
through `core:rust` with its default rustc, Cargo, rustfmt, Clippy, and docs
profile. The curated developer set includes gh `2.97.0`, rg `15.2.0`, fd
`10.4.2`, ast-grep `0.45.0`, sem `0.21.0`, and inspect `0.1.1`. The last
three use exact GitHub release backends plus architecture-specific asset
checksums; inspect's bare binary is also hashed before doctor or bootstrap
executes it. Existing hosts with the deprecated active `sg` mise key migrate to
`ast-grep`; mise removes that key and prunes its old install when unused. It
also includes digest-verified Herdr plus its Pi lifecycle
integration. Checksum-pinned GitHub tools publish Linux x86_64 and arm64 assets
only, so the default `all` toolset (and doctor) requires one of those
architectures; bootstrap rejects any other host before making changes. The
focused `git` target installs Git/OpenSSH plus target-user mise and gh before
interactive GitHub setup. Full development access requires a masked PAT and
stores gh credentials only on the managed host. Pi and optional inspect
authentication remain operator tasks. Serverpro updates Herdr only during
managed bootstrap and never starts, stops, or restarts Herdr sessions.
`serverpro server doctor` reports stale exact pins, cached apt candidates for
managed packages, and the Tailscale client/daemon release. `--fix` refreshes and
upgrades those paths; a Tailscale repair schedules its daemon restart only after
the updating Tailscale SSH command returns. See `USAGE.md` for bootstrap targets
and the doctor flow.

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

```sh
go install github.com/sagmans/serverpro/cmd/serverpro@latest
serverpro --version
```

## Uninstall

Remove the binary. Delete managed servers first when you no longer need the
provider resources:

```sh
serverpro server delete webapp -n mynamespace -p hetzner --dry-run
serverpro server delete webapp -n mynamespace -p hetzner --yes
```
