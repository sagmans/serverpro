# Installation

serverpro is a Go CLI for provisioning hardened Ubuntu servers. Start with a
dry-run, then move to live provider calls only after credentials and recovery
paths are ready.

## Supported platforms and versions

Reviewed 2026-08-27. These are the tested support boundaries, not a claim that
other environments cannot run the binary.

| Role | Supported platform | Architectures | Notes |
| --- | --- | --- | --- |
| Controller CLI | macOS 27 | arm64 | Runs provider APIs, local state, Tailscale SSH, and remote orchestration. |
| Managed host | Ubuntu 24.04 LTS (`noble`) | amd64, arm64 | Cloud-init, bootstrap, Tailscale, Docker, and Cloudflare Tunnel target. |
| Source build toolchain | Go 1.26.6 or newer | arm64 | Runs on the supported controller; Go 1.26.6 is the development, CI, and release baseline. |

Live create refetches the selected provider location's catalog and rejects a
missing image or any image outside this OS/release/architecture matrix before
provider mutation. Remote bootstrap, Tailscale, and Cloudflare installers verify
the actual host before managed mutation; cloud-init repeats the check as defense
in depth. An explicit provider `--image` value does not widen support. Provider
image identifiers differ: use `ubuntu-24.04` on Hetzner, an Ubuntu 24.04 numeric
OS ID on Vultr, and an Ubuntu 24.04 slug such as `ubuntu-24-04-x64` or its arm64
counterpart on DigitalOcean. Confirm current catalog availability before create.

### Managed tool baseline

| Tool | Supported version | Enforcement |
| --- | --- | --- |
| Tailscale | 1.102.3 | Exact client/daemon release and architecture-specific archive digest. |
| Git | 1:2.43.0-1ubuntu7.3 | Minimum Ubuntu package version. |
| OpenSSH client | 1:9.6p1-3ubuntu13.18 | Minimum Ubuntu package version. |
| Docker Engine / CLI | 29.7.2 | Minimum vendor package version `5:29.7.2-1~ubuntu.24.04~noble`. |
| containerd | 2.3.3 | Minimum vendor package version `2.3.3-1~ubuntu.24.04~noble`. |
| Docker Buildx | 0.36.1 | Minimum vendor package version `0.36.1-1~ubuntu.24.04~noble`. |
| Docker Compose | 5.5.0 | Minimum vendor package version `5.5.0-1~ubuntu.24.04~noble`. |
| htop | 3.3.0 | Minimum Ubuntu package version `3.3.0-4build1`. |
| mise | 2026.8.14 | Minimum release; newer compatible mise remains installed. |
| Node.js | 24.20.0 LTS | Exact mise-managed runtime. |
| npm | 11.19.0 | Exact npm bundled with the managed Node.js release. |
| Pi | 0.84.3 | Exact global package under managed Node.js. |
| uv | 0.12.6 | Exact mise-managed release. |
| Rust | 1.98.0 | Exact rustup toolchain with default profile. |
| tmux | 3.7c | Exact mise-managed release. |
| GitHub CLI (`gh`) | 2.98.0 | Exact mise-managed release. |
| ripgrep (`rg`) | 15.2.0 | Exact mise-managed release. |
| fd | 10.5.0 | Exact mise-managed release. |
| ast-grep | 0.45.2 | Exact release and architecture-specific asset digest. |
| sem | 0.23.1 | Exact release and architecture-specific asset digest. |
| inspect | 0.1.1 | Exact architecture-specific binary digest. |
| Herdr | 0.8.2 | Exact release and architecture-specific binary digest. |
| cloudflared | 2026.8.2 | Minimum Cloudflare apt package version when ingress is enabled. |

### Managed apt package floors

Every package installed directly by serverpro has a reviewed minimum version.
After repository refresh, serverpro verifies that each needed signed candidate
meets its floor before package scripts run, then verifies the installed result.
A fixed C locale keeps apt candidate parsing independent of host language. Only
dpkg state `installed` satisfies a floor; stale version metadata from a
removed package in `config-files` state triggers candidate validation instead.
Existing newer packages remain valid and are never downgraded; Ubuntu and vendor
security updates therefore continue normally.

| Package | Minimum version |
| --- | --- |
| `ca-certificates` | `20260601~24.04.1` |
| `curl` | `8.5.0-2ubuntu10.13` |
| `gnupg` | `2.4.4-2ubuntu17.4` |
| `ufw` | `0.36.2-6` |
| `apparmor` | `4.0.1really4.0.1-0ubuntu0.24.04.7` |
| `unattended-upgrades` | `2.9.1+nmu4ubuntu1` |
| `jq` | `1.7.1-3ubuntu0.24.04.2` |
| `git` | `1:2.43.0-1ubuntu7.3` |
| `openssh-client` | `1:9.6p1-3ubuntu13.18` |
| `docker-ce` | `5:29.7.2-1~ubuntu.24.04~noble` |
| `docker-ce-cli` | `5:29.7.2-1~ubuntu.24.04~noble` |
| `containerd.io` | `2.3.3-1~ubuntu.24.04~noble` |
| `docker-buildx-plugin` | `0.36.1-1~ubuntu.24.04~noble` |
| `docker-compose-plugin` | `5.5.0-1~ubuntu.24.04~noble` |
| `htop` | `3.3.0-4build1` |
| `cloudflared` | `2026.8.2` |

## Requirements

- A supported controller from the matrix above
- Go 1.26.6+
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

Each published target archive has a matching SPDX SBOM, provenance bundle, and
SBOM bundle. Publication does not widen the tested runtime matrix above; the
supported controller target is `darwin-arm64`. Select one of `linux-amd64`,
`linux-arm64`, `darwin-amd64`, or `darwin-arm64`, then download only that
target's files:

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

Create installs the exact Tailscale release, then `serverpro server bootstrap
NAME all` converges the complete tool and package baseline above. Run
`serverpro server bootstrap --help` for the code-owned target list. Focused
targets are `git`, `docker`, `mise`, `node`, and `pi`; `git` includes
Git/OpenSSH, target-user mise, and gh before interactive GitHub setup.

Checksum-pinned GitHub artifacts support only managed-host amd64 and arm64.
Existing hosts with the deprecated active `sg` mise key migrate to `ast-grep`.
Herdr includes its target-user Pi integration, but serverpro never starts,
stops, or restarts Herdr sessions. Pi and optional inspect authentication remain
operator tasks; full GitHub development access requires a masked PAT stored only
on the managed host.

`serverpro server doctor` reports exact-pin drift, packages below their reviewed
floors, newer apt candidates, Tailscale client/daemon drift, and cloudflared
floor drift when ingress is enabled. Its first remote check validates the actual
supported platform; failure disables every `--fix` mutation. On supported hosts,
`--fix` refreshes and upgrades the general managed package/tool paths; Tailscale repair delays its daemon restart until the
updating Tailscale SSH command returns. Cloudflared remains ingress-owned and is
diagnosed but not upgraded by generic bootstrap repair. See `USAGE.md` for the
operator flow.

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
  serverpro location list -p hetzner --non-interactive
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
