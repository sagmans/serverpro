# Web Sources for serverpro

Date: 2026-08-11
Scope: provider-agnostic serverpro references

Prefer official provider/API docs. If guides conflict with API references, trust
API references.

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
https://github.com/tailscale/tailscale/releases/tag/v1.102.2
https://github.com/tailscale/tailscale/issues/20067
```

Use: devices, keys, policy read, policy validate, policy update, and pinned
first-boot/live-repair binaries. Reviewed 2026-08-10: stable release `1.102.2`;
amd64 tarball SHA-256
`ad2cde12f8de95f7b93a1e0401e652291c603d42b9d60a33fb1741eb38ab04d8`;
arm64 tarball SHA-256
`2b64e9ade7e73034b5ec9e9bcd537f5ddd14ae3abb435e57e929e7486ae42660`.
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
```

Use: connector install source and apt trust root. Primary package-signing key
fingerprint retrieved and verified 2026-07-23:
`CC94B39C77AE7342A68B89628A682D308D4E5E73`. Cloudflare announced a 2025 key
rollover and removal of deprecated keys after 2026-04-30. On future rollover,
verify the replacement primary fingerprint from the official endpoint before
updating code; never trust a newly downloaded key merely because the package
endpoint serves it.

### Ubuntu security

```text
https://documentation.ubuntu.com/server/how-to/security/
```

Use: host hardening baseline.

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
```

Use: optional server tool bootstrap. Docker's apt signing key is pinned to
fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`.

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
https://github.com/jdx/mise/releases/tag/v2026.8.3
https://api.github.com/repos/jdx/mise/releases/tags/v2026.8.3
```

Use: mise prerequisite, scoped managed-tool installation, legacy managed-tool
removal, and pinned minimum version `2026.8.3`. Pinned release artifact
SHA-256 values:

- Linux x64: `8aaf21cc4b36681e90a96e9cdf13e5d7511e9773733f741b1a5f7756ba53b5fc`
- Linux arm64: `8d0c6142607d814279de0e06f53c9e896b5d267bbced9ee6e2d9e1547fccca8f`
- Linux armv7: `0b9f93b634e01c37b982e687915749c01265b9a084ce115e6a2b1b9c95c4e9d3`

### mise bootstrap

```text
https://mise.jdx.dev/bootstrap.html
https://mise.jdx.dev/cli/bootstrap.html
```

Use: managed package convergence through `mise bootstrap packages apply` and
scoped managed-tool installs through `mise install`.

### Node.js

```text
https://nodejs.org/en/blog/release/v24.19.0
https://nodejs.org/en/blog/vulnerability/july-2026-security-releases/
```

Use: pinned Node `24.19.0` LTS runtime and bundled npm `11.17.0`; the release
stays on the existing major line.

### uv

```text
https://github.com/astral-sh/uv/releases/tag/0.12.3
https://docs.astral.sh/uv/getting-started/installation/
```

Use: pinned uv `0.12.3` through mise's explicit `aqua:astral-sh/uv` backend.

### Rust and rustup

```text
https://blog.rust-lang.org/2026/07/16/Rust-1.97.1/
https://doc.rust-lang.org/stable/releases.html
https://mise.jdx.dev/lang/rust.html
https://rust-lang.github.io/rustup/security.html
```

Use: pinned Rust `1.97.1` through mise's `core:rust` backend and rustup default
profile. Reviewed 2026-07-31; Rust `1.97.1` fixes an LLVM miscompilation. rustup
uses HTTPS for downloads but does not yet enforce download signatures.

### mise npm backend

```text
https://mise.jdx.dev/dev-tools/backends/npm.html
```

Use: reference for npm-backed mise tools. serverpro does NOT use the `npm:`
backend; it installs Pi `0.84.1` via `npm install -g` under mise-managed Node
`24.19.0` with lifecycle-script suppression
(`npm_config_ignore_scripts=true`).

### Pi quickstart

```text
https://pi.dev/docs/latest/quickstart
```

Use: optional pinned Pi `0.84.1` bootstrap; authentication remains operator-owned.

### tmux

```text
https://github.com/tmux/tmux/releases
https://github.com/tmux/tmux/wiki/Installing
```

Use: pinned tmux `3.7b` build/install reference.

### Herdr

```text
https://herdr.dev/docs/install/
https://herdr.dev/docs/integrations/
https://github.com/herdrdev/herdr/releases/tag/v0.8.0
https://api.github.com/repos/herdrdev/herdr/releases/tags/v0.8.0
```

Use: managed Herdr `0.8.0` installation through mise's explicit GitHub backend,
package-manager update ownership, and target-user Pi integration status. Pinned
release SHA-256 values:

- Linux x64: `b872ea7e40fa2cb17e857ac9b62b1bf26db7b403c622f5d2f3f5b35f6e9acd28`
- Linux arm64: `f647ac66468d9efbc642fe534fb284468f0aea60641606fc008dfc0d82a3ca87`

### GitHub CLI

```text
https://cli.github.com/manual/
https://github.com/cli/cli/releases
```

Use: pinned `gh` `2.97.0` install reference; authentication remains operator-owned.

### ripgrep

```text
https://github.com/BurntSushi/ripgrep/releases
```

Use: pinned `rg` `15.2.0` tool reference.

### fd

```text
https://github.com/sharkdp/fd/releases
```

Use: pinned `fd` `10.4.2` tool reference.

### ast-grep

```text
https://ast-grep.github.io/
https://github.com/ast-grep/ast-grep/releases/tag/0.45.1
https://api.github.com/repos/ast-grep/ast-grep/releases/tags/0.45.1
```

Use: pinned ast-grep `0.45.1` through mise's GitHub backend. Pinned release
SHA-256 values:

- Linux x64: `76fb6555be6734fb5057dba8d2fb756430f374bb9e1af694cf1ce00e13238d63`
- Linux arm64: `9ee7ec49aada3dc05135d21977af089a33fc3154ada25bab102daca90b5098f2`

### sem

```text
https://github.com/Ataraxy-Labs/sem
https://github.com/Ataraxy-Labs/sem/releases/tag/v0.21.0
https://api.github.com/repos/Ataraxy-Labs/sem/releases/tags/v0.21.0
```

Use: pinned Ataraxy Labs sem `0.21.0` through mise's GitHub backend. Pinned
release SHA-256 values:

- Linux x64: `4a06f019552add37b4b0693309daaf529eae7f291217d20c291294c790b16b4b`
- Linux arm64: `0480663055d3d7c386dabee6e57766205984ac151bd691540bde0b3be64af27b`

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
https://packages.ubuntu.com/search?keywords=htop
https://packages.debian.org/search?keywords=htop
```

Use: htop availability and apt package path reference.

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
