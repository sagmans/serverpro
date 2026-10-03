# Web Sources for serverpro

Date: 2026-10-03
Scope: provider-agnostic serverpro references

Prefer official provider/API docs. If guides conflict with API references, trust
API references.

## Managed-tool review: 2026-10-03

The normal cutoff is 2026-09-26 09:13:29 UTC: stable releases must be at least
seven days old. Preserve Ubuntu 24.04 packages and Node.js 24 LTS. Keep npm
11.19.0 bundled with Node 24.21.0 instead of independently upgrading npm to 12.
Unchanged pins remain when no eligible compatible update exists.
The 2026-10-03 refresh changes only uv and the security-fixed mise minimum.
Newer Tailscale `1.102.5`, Compose `5.6.0`, Rust `1.99.0`, Herdr `0.9.3`,
sem `0.26.0`, and Pi `1.0.0` remain younger than seven days.
containerd `2.4.1` is old enough upstream but absent from Docker’s Noble packages;
retain vendor-compatible `2.3.6`. Do not downgrade approved security floors.
Mise `2026.10.0` fixes medium-severity `GHSA-wcqh-j26q-g44x`, published 2026-10-02.
Both mise GNU/Linux archives were downloaded independently and matched published checksums.

| Updated tool | Selected release | Published (UTC) | Selection |
| --- | --- | --- | --- |
| Tailscale | 1.102.4 | 2026-09-10 | Normal age gate |
| Docker Engine / CLI | 29.8.2 | 2026-09-30 | Approved security exception |
| containerd | 2.3.6 | 2026-09-24 23:13:08 | Normal age gate; preserve Docker's 2.3 package line |
| Buildx | 0.37.2 | 2026-09-30 | Approved security exception; apt publication pending |
| Compose | 5.5.1 | 2026-09-03 | Normal age gate |
| mise | 2026.10.0 | 2026-10-02 01:42:32 | Approved security exception; fixes inline-options token exfiltration |
| Node.js | 24.21.0 LTS | 2026-09-07 | Normal age gate |
| Pi | 0.87.1 | 2026-09-22 | Normal age gate |
| uv | 0.12.19 | 2026-09-25 00:33:02 | Normal age gate; retains the wheel traversal fix |
| Rust | 1.98.1 | 2026-09-03 | Normal age gate |
| gh | 2.102.0 | 2026-09-30 | Approved security exception |
| ast-grep | 0.45.3 | 2026-08-31 | Normal age gate |
| sem | 0.25.0 | 2026-09-13 | Normal age gate |
| Herdr | 0.9.1 | 2026-09-16 | Normal age gate |
| cloudflared | 2026.9.3 | 2026-09-24 16:14:11 | Normal age gate |

Primary selection and security evidence:

- https://nodejs.org/dist/index.json
- https://registry.npmjs.org/@earendil-works%2Fpi-coding-agent
- https://api.launchpad.net/1.0/ubuntu/+archive/primary
- https://download.docker.com/linux/ubuntu/dists/noble/stable/binary-amd64/Packages.gz
- https://download.docker.com/linux/ubuntu/dists/noble/stable/binary-arm64/Packages.gz
- https://github.com/moby/moby/releases/tag/docker-v29.8.2
- https://github.com/containerd/containerd/releases/tag/v2.3.6
- https://github.com/containerd/containerd/security/advisories/GHSA-pg57-6jwg-q645
- https://github.com/docker/buildx/releases/tag/v0.37.2
- https://github.com/docker/buildx/security/advisories/GHSA-gwr2-q96m-6682
- https://github.com/docker/buildx/security/advisories/GHSA-p54p-jq4x-rc28
- https://github.com/jdx/mise/security/advisories/GHSA-333c-h2jr-xv83
- https://github.com/jdx/mise/security/advisories/GHSA-wcqh-j26q-g44x
- https://github.com/astral-sh/uv/releases/tag/0.12.19
- https://github.com/cli/cli/releases/tag/v2.102.0
- https://github.com/astral-sh/uv/security/advisories/GHSA-2cv4-cqwr-gwf7
- https://github.com/jdx/mise/releases/download/v2026.10.0/SHASUMS256.txt
- https://github.com/Ataraxy-Labs/sem/releases/download/v0.25.0/checksums.txt

The Docker repository contains Engine 29.8.2 and containerd 2.3.6 on both
architectures, but only Buildx 0.37.1. Require the patched Buildx floor and
reject unavailable candidates rather than install a known-vulnerable release.
The disposable Ubuntu arm64 installation verified every managed user-tool
version and the Herdr Pi integration. Pi `0.87.1` then failed npm audit: its
`npm-shrinkwrap.json` pins `brace-expansion@5.0.9`. Pi `1.0.0` contains the
same vulnerable dependency. Version `5.0.12` was published 2026-09-14 and fixes
the reported issues. Its registry SHA-512 and npm signatures were verified.
Serverpro now repairs its local Pi installation to that exact dependency
version and verifies the reviewed shrinkwrap integrity before use. A disposable
Ubuntu arm64 run passed real Pi help, idempotence, vulnerability rejection,
repair after drift, and the runtime dependency audit with zero vulnerabilities.

- https://github.com/advisories/GHSA-qhr7-859c-m2p7
- https://github.com/advisories/GHSA-6j4f-fj2g-mc7p
- https://github.com/advisories/GHSA-q2hr-2g5m-vwhr
- https://registry.npmjs.org/brace-expansion
- https://registry.npmjs.org/@earendil-works/pi-coding-agent/-/pi-coding-agent-0.87.1.tgz
- https://registry.npmjs.org/@earendil-works/pi-coding-agent/-/pi-coding-agent-1.0.0.tgz

These findings are a point-in-time review, not proof that all dependencies or
artifacts are safe. The local Pi repair passed its runtime dependency audit.
Buildx availability remains a deployment prerequisite.
Revalidate signed repository metadata before deployment.

## Core references

### Go toolchain and security floor

```text
https://go.dev/doc/devel/release
https://go.dev/doc/toolchain
https://go.dev/ref/mod
https://pkg.go.dev/vuln/GO-2026-5856
https://pkg.go.dev/vuln/GO-2026-4970
https://groups.google.com/g/golang-announce/c/94pEornpRlI
https://www.openwall.com/lists/oss-security/2026/08/13/13
https://pkg.go.dev/vuln/GO-2026-6218
https://pkg.go.dev/vuln/GO-2026-6090
https://pkg.go.dev/vuln/GO-2026-5972
https://pkg.go.dev/vuln/GO-2026-5026
```

Use: minimum supported Go version and official vulnerability evidence. Reviewed
2026-08-11: Go 1.26.5 fixes `crypto/tls` and `os` security issues affecting Go
1.26.0 through 1.26.4. Reviewed 2026-08-26: Go 1.26.6 (released 2026-08-13,
announced on golang-announce and oss-security) fixes ten security issues,
including the four standard-library advisories govulncheck reported reachable
from serverpro under Go 1.26.5 (`net/url`, `crypto/tls`, `encoding/asn1`, and
the vendored `x/net/idna` path). The `go` directive is the mandatory minimum
toolchain version, so source builds require Go 1.26.6 or newer.

### Go quality tools

```text
https://github.com/golangci/golangci-lint/releases/tag/v2.12.2
https://api.github.com/repos/golangci/golangci-lint/releases/tags/v2.12.2
https://golangci-lint.run/docs/product/migration-guide/
https://github.com/golang/vuln/tree/v1.6.0
https://api.github.com/repos/golang/vuln/git/ref/tags/v1.6.0
```

Use: project-local lint and vulnerability scanner pins. Reviewed 2026-08-11:
golangci-lint `v2.12.2` is an immutable stable release; govulncheck `v1.6.0`
is an exact official repository tag. Both require Go 1.25 or newer and support
the Go 1.26.6 project toolchain.

### Hetzner Cloud API

```text
https://docs.hetzner.cloud/
https://docs.hetzner.cloud/reference/cloud
https://docs.hetzner.cloud/reference/cloud#servers
https://docs.hetzner.cloud/reference/cloud#firewalls
https://docs.hetzner.cloud/reference/cloud#images
https://docs.hetzner.cloud/reference/cloud#locations
https://docs.hetzner.cloud/reference/cloud#server-types
```

Use: first compute adapter; servers, firewalls, images, locations, server types,
and actions. Hetzner server and firewall labels use the shared provider
ownership convention: `managed-by=serverpro`,
`serverpro-namespace=<namespace>`, and `serverpro-server=<server>`.

### Hetzner DNS

```text
https://www.hetzner.com/dns/
```

Use: provider DNS reference; DNS remains app/operator-owned.

### Vultr API

```text
https://www.vultr.com/api/
https://www.vultr.com/api/#tag/instances
https://www.vultr.com/api/#tag/plans
https://www.vultr.com/api/#tag/region
https://www.vultr.com/api/#tag/os
```

Use: compute adapter for instances, firewall groups, regions, plans, and OS.
Specific endpoints used:

- Instances: `POST /v2/instances`, `GET /v2/instances/{instance-id}`,
  `DELETE /v2/instances/{instance-id}`, `POST /v2/instances/{instance-id}/start`,
  `POST /v2/instances/{instance-id}/halt`, `POST /v2/instances/{instance-id}/reboot`.
- Firewall groups and rules: `POST /v2/firewalls`,
  `GET /v2/firewalls/{firewall-group-id}`,
  `DELETE /v2/firewalls/{firewall-group-id}`,
  `GET /v2/firewalls/{firewall-group-id}/rules`, and
  `POST /v2/firewalls/{firewall-group-id}/rules`.
- Catalog: `GET /v2/regions`, `GET /v2/plans`, `GET /v2/os`.

Vultr `user_data` is base64-encoded before submission. `enable_ipv6` is set to
`false` so instances receive IPv4 only. Vultr instance tags use the shared
provider ownership convention: `managed-by:serverpro`,
`serverpro-namespace:<namespace>`, and `serverpro-server:<server>`. Values with
provider-invalid tag characters are encoded reversibly.

### DigitalOcean API

```text
https://docs.digitalocean.com/reference/api/
https://docs.digitalocean.com/reference/api/reference/droplets/
https://docs.digitalocean.com/reference/api/reference/firewalls/
https://docs.digitalocean.com/reference/api/reference/tags/
https://docs.digitalocean.com/reference/api/reference/regions/
https://docs.digitalocean.com/reference/api/reference/sizes/
https://docs.digitalocean.com/reference/api/reference/images/
https://docs.digitalocean.com/reference/api/reference/domain-records/
```

Use: compute adapter for droplets, firewalls, regions, sizes, images, and
droplet actions. Specific endpoints used:

- Droplets: `POST /v2/droplets`, `GET /v2/droplets/{droplet_id}`,
  `GET /v2/droplets?name=<name>`, `DELETE /v2/droplets/{droplet_id}`.
- Droplet actions: `POST /v2/droplets/{droplet_id}/actions` with
  `power_on`, `shutdown`, and `reboot`.
- Firewalls: `POST /v2/firewalls`, `GET /v2/firewalls/{firewall_id}`,
  `DELETE /v2/firewalls/{firewall_id}`.
- Tags: `POST /v2/tags`, `GET /v2/tags/{tag_name}`.
- Catalog: `GET /v2/regions`, `GET /v2/sizes`,
  `GET /v2/images?type=distribution`.

DigitalOcean `user_data` is sent as raw cloud-init data. `ipv6` is set to
`false` so droplets receive IPv4 only. Droplets use the same shared provider
ownership convention as Vultr because DigitalOcean tag names allow letters,
numbers, colons, dashes, and underscores, not dots. Values with
provider-invalid tag characters are encoded reversibly. Each firewall uses
exactly one derived namespace/server target tag; the droplet keeps shared
ownership/custom tags plus that target tag. Recovery and deletion reject
additional tags and direct droplet-ID attachments. The firewall is created
before the droplet, keeps SSH closed, and allows Tailscale UDP ports `41641`
and `3478` inbound.

### Tailscale API and first-boot artifacts

```text
https://tailscale.com/docs/reference/tailscale-api
https://tailscale.com/docs/reference/dns-in-tailscale
https://tailscale.com/docs/features/magicdns
https://tailscale.com/docs/reference/linux-dns
https://tailscale.com/docs/reference/faq/dns-resolv-conf
https://tailscale.com/kb/1337/acl-syntax
https://pkgs.tailscale.com/stable/
https://github.com/tailscale/tailscale/releases/tag/v1.102.4
https://github.com/tailscale/tailscale/issues/20067
```

Use: devices, keys, policy read, policy validate, policy update, and pinned
first-boot/live-repair binaries. Reviewed 2026-10-01: stable release `1.102.4`;
amd64 tarball SHA-256
`50748df1045e60b5b695f19f4c56b0da36c019948b440fb456b6584a50f0d8b9`;
arm64 tarball SHA-256
`9dd1e6a592a014bbaea0103167ffe299adeda4ba14e078ce9c2895364f6c4c3f`.
Serverpro supplies `GODEBUG=tlsmlkem=1` to the systemd service independently
from the artifact build default. Recheck the stable release, checksums,
advisory state, and binary build setting together when rotating this pin.

### Tailscale SSH

```text
https://tailscale.com/kb/1193/tailscale-ssh
```

Use: mandatory admin path and SSH policy behavior.

### Tailscale auth keys

```text
https://tailscale.com/kb/1085/auth-keys
```

Use: one-off tagged keys.

### Tailscale tags

```text
https://tailscale.com/kb/1068/tags
```

Use: server identity and tag ownership.

### Cloudflare API

```text
https://developers.cloudflare.com/api/
https://developers.cloudflare.com/api/resources/dns/subresources/records/
```

Use: account, DNS, and tunnel endpoints.

### Remote tunnel API

```text
https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel-api/
```

Use: Cloudflare Tunnel adapter behavior.

### Tunnel tokens

```text
https://developers.cloudflare.com/tunnel/advanced/tunnel-tokens/
```

Use: connector token handling.

### Cloudflare Tunnel routing

```text
https://developers.cloudflare.com/tunnel/routing/
https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-with-firewall/
```

Use: optional ingress route model and outbound firewall guidance.

### Cloudflare Tunnel configuration file

```text
https://developers.cloudflare.com/tunnel/advanced/local-management/configuration-file/
```

Use: ingress rule shape and service targets.

### cloudflared downloads and package signing

```text
https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/
https://pkg.cloudflare.com/index.html
https://pkg.cloudflare.com/cloudflare-main.gpg
https://pkg.cloudflare.com/cloudflared/dists/noble/main/binary-amd64/Packages
https://pkg.cloudflare.com/cloudflared/dists/noble/main/binary-arm64/Packages
https://github.com/cloudflare/cloudflared/releases/tag/2026.9.3
```

Use: connector install source, Ubuntu 24.04 package floor `2026.9.3`, and apt
trust root. Package indexes and release reviewed 2026-10-01. Primary
package-signing key fingerprint retrieved and verified 2026-07-23:
`CC94B39C77AE7342A68B89628A682D308D4E5E73`. Cloudflare announced a 2025 key
rollover and removal of deprecated keys after 2026-04-30. On future rollover,
verify the replacement primary fingerprint from the official endpoint before
updating code; never trust a newly downloaded key merely because the package
endpoint serves it.

### Ubuntu 24.04 support and package floors

```text
https://documentation.ubuntu.com/server/how-to/security/
https://manpages.debian.org/bookworm/dpkg/dpkg-query.1.en.html
https://packages.ubuntu.com/noble-updates/ca-certificates
https://packages.ubuntu.com/noble-updates/curl
https://packages.ubuntu.com/noble-updates/gnupg
https://packages.ubuntu.com/noble/ufw
https://packages.ubuntu.com/noble-updates/apparmor
https://packages.ubuntu.com/noble/unattended-upgrades
https://packages.ubuntu.com/noble-updates/jq
https://packages.ubuntu.com/noble-updates/git
https://packages.ubuntu.com/noble-updates/openssh-client
https://packages.ubuntu.com/noble/htop
```

Use: Ubuntu 24.04 LTS (`noble`) managed-host support, host hardening, and direct
package minimums. Reviewed 2026-10-01 for amd64 and arm64: `ca-certificates`
`20260601~24.04.1`, `curl` `8.5.0-2ubuntu10.15`, `gnupg`
`2.4.4-2ubuntu17.6`, `ufw` `0.36.2-6`, `apparmor`
`4.0.1really4.0.1-0ubuntu0.24.04.8`, `unattended-upgrades`
`2.9.1+nmu4ubuntu1`, `jq` `1.7.1-3ubuntu0.24.04.2`, `git`
`1:2.43.0-1ubuntu7.3`, `openssh-client` `1:9.6p1-3ubuntu13.19`, and `htop`
`3.3.0-4build1`. These are reviewed floors; signed newer candidates remain
valid and must not be downgraded. `dpkg-query -W` can retain version data for a
removed package in `config-files` state, so package-floor checks require
`${db:Status-Status}` to be exactly `installed` before trusting `${Version}`.

### UFW

```text
https://documentation.ubuntu.com/server/how-to/security/firewalls/
```

Use: host firewall checks.

### OpenSSH

```text
https://documentation.ubuntu.com/server/how-to/security/openssh-server/
```

Use: public, root, and password SSH hardening.

### sudo

```text
https://www.sudo.ws/docs/man/sudo.man/
```

Use: password-aware sudo execution.

### sudoers

```text
https://www.sudo.ws/docs/man/sudoers.man/
```

Use: `NOPASSWD` and `PASSWD` semantics.

### cloud-init

```text
https://docs.cloud-init.io/en/latest/reference/index.html
```

Use: first-boot bootstrap data.

### cloud-init users/groups

```text
https://docs.cloud-init.io/en/24.4/reference/yaml_examples/user_groups.html
```

Use: admin user and hashed password config.

### DigitalOcean droplet user data

```text
https://docs.digitalocean.com/products/droplets/how-to/provide-user-data/
```

Use: cloud-init/user-data behavior for DigitalOcean droplets.

### DigitalOcean recovery console

```text
https://docs.digitalocean.com/products/droplets/how-to/recovery/recovery-console/
```

Use: emergency recovery guidance.

### Vultr cloud-init

```text
https://docs.vultr.com/products/compute/instances/cloud-compute/features/cloud-init
```

Use: cloud-init/user-data behavior for Vultr instances.

### Vultr firewall groups

```text
https://docs.vultr.com/products/network/firewall-groups/
```

Use: provider firewall behavior.

### Vultr DNS

```text
https://docs.vultr.com/products/network/dns/
```

Use: provider DNS reference; DNS remains app/operator-owned.

### Docker Engine Ubuntu

```text
https://docs.docker.com/engine/install/ubuntu/
https://download.docker.com/linux/ubuntu/gpg
https://download.docker.com/linux/ubuntu/dists/noble/pool/stable/amd64/
https://download.docker.com/linux/ubuntu/dists/noble/pool/stable/arm64/
```

Use: managed Docker bootstrap. Docker's apt signing key is pinned to fingerprint
`9DC858229FC7DD38854AE2D88D81803C0EBFCD88`. Reviewed 2026-10-01 floors for
Ubuntu 24.04 amd64/arm64: Docker Engine and CLI
`5:29.8.2-1~ubuntu.24.04~noble`, containerd.io
`2.3.6-1~ubuntu.24.04~noble`, Buildx
`0.37.2-1~ubuntu.24.04~noble`, and Compose
`5.5.1-1~ubuntu.24.04~noble`.

### Docker Linux postinstall

```text
https://docs.docker.com/engine/install/linux-postinstall/
```

Use: Docker socket access caveats.

### mise install

```text
https://mise.jdx.dev/installing-mise.html
https://mise.jdx.dev/cli/install.html
https://mise.jdx.dev/cli/unuse.html
https://mise.jdx.dev/dev-tools/backends/github.html
https://github.com/jdx/mise/releases/tag/v2026.10.0
https://api.github.com/repos/jdx/mise/releases/tags/v2026.10.0
```

Use: mise prerequisite, scoped managed-tool installation, legacy managed-tool
removal, and reviewed minimum version `2026.10.0`. Pinned release artifact
SHA-256 values:

- Linux x64: `6ae3d2bda39cca86713501317edf623b59b75cd8afa2e31c20b1ee6500b4b739`
- Linux arm64: `107c5e46693cdfeb1fdec91717078b298d6fcc9ebbd14f8333917cfe37965138`

### mise bootstrap

```text
https://mise.jdx.dev/bootstrap.html
https://mise.jdx.dev/cli/bootstrap.html
```

Use: managed package convergence through `mise bootstrap packages apply` and
scoped managed-tool installs through `mise install`.

### Node.js

```text
https://nodejs.org/en/blog/release/v24.21.0
https://nodejs.org/en/about/previous-releases
https://nodejs.org/en/blog/vulnerability/july-2026-security-releases/
```

Use: pinned Node `24.21.0` LTS runtime and bundled npm `11.19.0`; reviewed
2026-10-01 on the supported Node 24 LTS line.

### uv

```text
https://github.com/astral-sh/uv/releases/tag/0.12.19
https://docs.astral.sh/uv/getting-started/installation/
```

Use: pinned uv `0.12.19` through mise's explicit `aqua:astral-sh/uv` backend;
reviewed 2026-10-03. Independently downloaded GNU/Linux assets match their
published SHA-256 files: x86_64 `23bf5552d220e0842b65c862097b2ebaeba0064b74eda5e565e77fd25969d8c8`;
aarch64 `0804e9b164c64b6914182d5920c08551958a095986f10a3731056df701126436`.

### Rust and rustup

```text
https://blog.rust-lang.org/2026/09/03/Rust-1.98.1/
https://doc.rust-lang.org/stable/releases.html
https://mise.jdx.dev/lang/rust.html
https://rust-lang.github.io/rustup/security.html
```

Use: pinned Rust `1.98.1` through mise's `core:rust` backend and rustup default
profile. Reviewed 2026-10-01. rustup uses HTTPS for downloads but does not yet
enforce download signatures.

### mise npm backend

```text
https://mise.jdx.dev/dev-tools/backends/npm.html
```

Use: reference for npm-backed mise tools. serverpro does NOT use the `npm:`
backend; it installs Pi `0.87.1` via `npm install -g` under mise-managed Node
`24.21.0` with lifecycle-script suppression
(`npm_config_ignore_scripts=true`).

### Pi quickstart

```text
https://pi.dev/docs/latest/quickstart
https://www.npmjs.com/package/@earendil-works/pi-coding-agent/v/0.87.1
```

Use: optional pinned Pi `0.87.1` bootstrap; reviewed 2026-10-01.
Authentication remains operator-owned.

### tmux

```text
https://github.com/tmux/tmux/releases/tag/3.7c
https://github.com/tmux/tmux/wiki/Installing
```

Use: pinned tmux `3.7c` build/install reference; reviewed 2026-10-01.

### Herdr

```text
https://herdr.dev/docs/install/
https://herdr.dev/docs/integrations/
https://github.com/herdrdev/herdr/releases/tag/v0.9.1
https://api.github.com/repos/herdrdev/herdr/releases/tags/v0.9.1
```

Use: managed Herdr `0.9.1` installation through mise's explicit GitHub backend,
package-manager update ownership, and target-user Pi integration status.
Reviewed 2026-10-01. Pinned release SHA-256 values:

- Linux x64: `2a02fed16beb651ef006e1d43f048f652ca4dc58ad053cd2d44450563d5c54b7`
- Linux arm64: `f4ccf4de745f2cb9a39a983e9ba3703dad50ec2a58dea83026ceab721bbd8d9e`

### GitHub CLI

```text
https://cli.github.com/manual/
https://github.com/cli/cli/releases/tag/v2.102.0
```

Use: pinned `gh` `2.102.0` install reference; reviewed 2026-10-01.
Authentication remains operator-owned.

### ripgrep

```text
https://github.com/BurntSushi/ripgrep/releases
```

Use: pinned `rg` `15.2.0` tool reference.

### fd

```text
https://github.com/sharkdp/fd/releases/tag/v10.5.0
```

Use: pinned `fd` `10.5.0` tool reference; reviewed 2026-10-01.

### ast-grep

```text
https://ast-grep.github.io/
https://github.com/ast-grep/ast-grep/releases/tag/0.45.3
https://api.github.com/repos/ast-grep/ast-grep/releases/tags/0.45.3
```

Use: pinned ast-grep `0.45.3` through mise's GitHub backend; reviewed
2026-10-01. Pinned release SHA-256 values:

- Linux x64: `f8ac830881339d1edee6b2652f54798c0f4da5a827f2db38a08ee31117783ce8`
- Linux arm64: `b39cfbc58da4b869a88b8a4bc57bd5deb0d24541e704cf7c257da7b53ec81c8f`

### sem

```text
https://github.com/Ataraxy-Labs/sem
https://github.com/Ataraxy-Labs/sem/releases/tag/v0.25.0
https://api.github.com/repos/Ataraxy-Labs/sem/releases/tags/v0.25.0
```

Use: pinned Ataraxy Labs sem `0.25.0` through mise's GitHub backend; reviewed
2026-10-01. Pinned release SHA-256 values:

- Linux x64: `7151f577f84eef16b32ab67bbf4336b1878de1f4d7a087583b0a08ee4e0fb4fd`
- Linux arm64: `ce0ab8f8c7bbacde1c8ed87e5a10b4cd10737870597285da8cd62e905ad3bd8c`

### inspect

```text
https://github.com/Ataraxy-Labs/inspect
https://github.com/Ataraxy-Labs/inspect/releases/tag/v0.1.1
https://api.github.com/repos/Ataraxy-Labs/inspect/releases/tags/v0.1.1
```

Use: pinned Ataraxy Labs inspect `0.1.1` through mise's GitHub backend. Upstream
has no version flag, so serverpro hashes the bare binary before execution.
Pinned binary SHA-256 values:

- Linux x64: `99cf4ea2a2a1048d8e9369a6a5a11e5f84ee3f3c706e0bde072f9b2bd44e96ba`
- Linux arm64: `2327c1de10ecf40e5199c15fdc4c4b3c173735640294e779c635f4c15771e4f6`

### htop

```text
https://htop.dev/
https://packages.ubuntu.com/noble/htop
```

Use: htop `3.3.0-4build1` Ubuntu 24.04 package floor; reviewed 2026-10-01.

### Twelve-Factor config

```text
https://12factor.net/config
```

Use: app-owned config and environment separation.

### Twelve-Factor build/release/run

```text
https://12factor.net/build-release-run
```

Use: app-owned deployment boundary.

### OWASP Secrets Management

```text
https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html
https://cheatsheetseries.owasp.org/cheatsheets/Key_Management_Cheat_Sheet.html
https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
```

Use: future credential storage hardening.

### CIS Ubuntu Linux Benchmark

```text
https://www.cisecurity.org/benchmark/ubuntu_linux
```

Use: reputable host hardening reference.

### NIST SP 800-123

```text
https://csrc.nist.gov/pubs/sp/800/123/final
```

Use: general server security guidance.

### GitHub security policy

```text
https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/configure-vulnerability-reporting/adding-a-security-policy-to-your-repository
https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/report-a-vulnerability/privately-reporting-a-security-vulnerability
```

Use: vulnerability reporting policy.

### GitHub releases and attestations

```text
https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases
https://docs.github.com/en/actions/use-cases-and-examples/building-and-testing/building-and-testing-go
https://docs.github.com/en/actions/reference/runners/github-hosted-runners
https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations
https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/verify-attestations-offline
https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/secure-your-dependencies/verify-release-integrity
https://cli.github.com/manual/gh_attestation_verify
https://github.com/actions/checkout/releases/tag/v7.0.1
https://github.com/actions/setup-go/releases/tag/v7.0.0
https://github.blog/changelog/2025-09-19-deprecation-of-node-20-on-github-actions-runners/
https://github.com/actions/upload-artifact/releases/tag/v7.0.1
https://github.com/actions/download-artifact/releases/tag/v8.0.1
https://github.com/actions/attest-build-provenance/releases/tag/v4.1.1
https://github.com/actions/attest/releases/tag/v4.2.0
https://github.com/anchore/sbom-action/releases/tag/v0.24.0
https://github.com/anchore/syft/releases/tag/v1.48.0
```

Use: release workflow, native runner architecture, artifact transport, SPDX
SBOM, signed provenance, attached-bundle verification, and release-integrity
guidance. Reviewed 2026-08-11: checkout is pinned to `v7.0.1` and setup-go to
`v7.0.0`; both declare the supported Node 24 JavaScript action runtime, and the
release runner uses Go `1.26.6`. Upload/download/attestation actions are pinned
to the tags listed above, with commit SHAs recorded in workflow comments; Syft
is pinned to `v1.48.0`.
`macos-15-intel` supplies amd64 and `macos-15` supplies arm64 for native smoke
execution. Recheck action release notes, runtime requirements, and Syft security
advisories before rotating any pin.

### Hetzner VNC

```text
https://docs.hetzner.com/cloud/servers/getting-started/vnc-console
```

Use: emergency recovery guidance.

### Hetzner Rescue

```text
https://docs.hetzner.com/cloud/servers/getting-started/rescue-system/
```

Use: emergency recovery guidance.

### Vultr Console

```text
https://docs.vultr.com/products/compute/cloud-compute/connection/vultr-console
```

Use: emergency recovery guidance.

## Implementation notes

- Provider APIs are accessed through adapters behind the generic compute facade.
- Shared HTTP behavior belongs in `internal/provider/httpjson`.
- Hetzner-specific references apply only to `internal/provider/hetzner`.
- Tailscale SSH remains the default admin path.
- Public app ingress is opt-in. Default ingress is `none`.
- Cloudflare Tunnel is an ingress adapter, not a compute provider feature.
- Cloudflare Tunnel ingress must not require inbound compute firewall openings.
- Keep provider tokens, tunnel tokens, auth keys, sudo passwords, password
  hashes, and bootstrap data secret.
- Use best-effort egress language only.
- Do not claim domain-precise UFW or provider firewalling.
- App deployment, environment files, DNS content, and public routes remain
  app-owned unless a future threat model changes scope.
