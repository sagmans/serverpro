# Web Sources for serverpro

Date: 2026-08-04
Scope: provider-agnostic serverpro references

Prefer official provider/API docs. If guides conflict with API references, trust
API references.

## Core references

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
https://docs.vultr.com/how-to-provision-cloud-infrastructure-on-vultr-using-terraform
```

Use: compute adapter for instances, firewall groups, regions, plans, and OS.
The official provisioning guide identifies OS ID `2284` as Ubuntu 24.04.
Specific endpoints used:

- Instances: `POST /v2/instances`, `GET /v2/instances/{instance-id}`,
  `DELETE /v2/instances/{instance-id}`, `POST /v2/instances/{instance-id}/start`,
  `POST /v2/instances/{instance-id}/halt`, `POST /v2/instances/{instance-id}/reboot`.
- Firewall groups: `POST /v2/firewalls`, `GET /v2/firewalls/{firewall-group-id}`,
  `DELETE /v2/firewalls/{firewall-group-id}`.
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
`false` so droplets receive IPv4 only. The adapter uses the same shared provider
ownership convention as Vultr because DigitalOcean tag names allow letters,
numbers, colons, dashes, and underscores, not dots. Values with
provider-invalid tag characters are encoded reversibly. It ensures ownership
tag resources exist, creates a tag-bound firewall before the droplet, keeps SSH
closed, and allows Tailscale UDP ports `41641` and `3478` inbound.

### Tailscale API

```text
https://tailscale.com/docs/reference/tailscale-api
https://tailscale.com/kb/1337/acl-syntax
```

Use: devices, keys, policy read, policy validate, and policy update.

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

### Tailscale Linux packages

```text
https://tailscale.com/docs/install/linux
https://pkgs.tailscale.com/stable/#ubuntu-noble
https://pkgs.tailscale.com/stable/ubuntu/noble.noarmor.gpg
```

Use: signed Ubuntu 24.04 package installation and package-key verification.

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

### cloudflared downloads

```text
https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/
https://pkg.cloudflare.com/index.html
https://pkg.cloudflare.com/cloudflare-main.gpg
```

Use: connector install source, signed apt repository, and package-key
verification.

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
```

Use: optional server tool bootstrap.

### Docker Linux postinstall

```text
https://docs.docker.com/engine/install/linux-postinstall/
```

Use: Docker socket access caveats.

### mise install

```text
https://mise.jdx.dev/installing-mise.html
https://mise.jdx.dev/cli/install.html
https://github.com/jdx/mise/releases/tag/v2026.7.12
https://api.github.com/repos/jdx/mise/releases/tags/v2026.7.12
```

Use: mise prerequisite, pinned release artifacts, and scoped managed-tool
installation.

### mise bootstrap

```text
https://mise.jdx.dev/bootstrap.html
https://mise.jdx.dev/cli/bootstrap.html
```

Use: managed package convergence through `mise bootstrap packages apply` and
scoped managed-tool installs through `mise install`.

### mise npm backend

```text
https://mise.jdx.dev/dev-tools/backends/npm.html
```

Use: reference for npm-backed mise tools. serverpro does NOT use the `npm:`
backend; it installs Pi via `npm install -g` under the mise-managed Node with
lifecycle-script suppression (`npm_config_ignore_scripts=true`).

### Pi quickstart

```text
https://pi.dev/docs/latest/quickstart
```

Use: optional Pi bootstrap; authentication remains operator-owned.

### tmux

```text
https://github.com/tmux/tmux/releases
https://github.com/tmux/tmux/wiki/Installing
```

Use: managed terminal multiplexer version and build/install reference.

### Herdr

```text
https://herdr.dev/docs/install/
https://herdr.dev/docs/integrations/
https://github.com/ogulcancelik/herdr/releases/tag/v0.7.5
https://api.github.com/repos/ogulcancelik/herdr/releases/tags/v0.7.5
```

Use: managed Herdr installation through mise's explicit GitHub backend, pinned
Linux release digests, package-manager update ownership, and target-user Pi
integration status.

### GitHub CLI

```text
https://cli.github.com/manual/
https://github.com/cli/cli/releases
```

Use: managed `gh` CLI install reference; authentication remains operator-owned.

### ripgrep

```text
https://github.com/BurntSushi/ripgrep/releases
```

Use: managed `rg` tool version reference.

### fd

```text
https://github.com/sharkdp/fd/releases
```

Use: managed `fd` tool version reference.

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

### GitHub releases and actions

```text
https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases
https://docs.github.com/en/actions/use-cases-and-examples/building-and-testing/building-and-testing-go
https://github.com/actions/checkout/releases/tag/v6.0.2
https://github.com/actions/setup-go/releases/tag/v6.4.0
```

Use: release workflow, Go build guidance, and mature security-hardened action
pins.

### Go analysis tools

```text
https://github.com/golangci/golangci-lint/releases/tag/v1.64.8
https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck@v1.5.0
```

Use: reproducible lint and vulnerability-analysis tool pins.

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
