# Web Sources for serverpro

Date: 2026-10-30
Scope: provider-agnostic serverpro references

Prefer official provider/API docs. If guides conflict with API references, trust
API references.

## Managed-tool review: 2026-10-03, refreshed 2026-10-22 and 2026-10-30

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
Reviewed 2026-10-22: the mise floor moves to `2026.10.17`, published 2026-10-21.
Its built-in npm backend no longer trusts eight eMudhra intermediate CA
certificates whose private keys were compromised. It is younger than seven
days; the owner approved this security exception. It keeps the
`GHSA-wcqh-j26q-g44x` fix.
Both mise GNU/Linux archives were downloaded independently and matched published checksums.

Reviewed 2026-10-22 (cutoff 2026-10-15 18:21 UTC): the mise-managed tool set
moves to Node.js `24.22.1` with bundled npm `11.19.1`, uv `0.13.5`, gh `2.102.1`,
ast-grep `0.51.0`, Rust `1.99.0`, sem `0.27.0`, and Herdr `0.9.5`. Node.js, uv,
and gh are younger than seven days; the owner approved them as security
exceptions. uv `0.13.0` changes how constraint and override options split paths;
serverpro does not call them. Every new GitHub asset was downloaded independently
and matched both its published checksum and its GitHub asset digest.
Pi stays on `0.87.1`: Pi `1.0.1` and later ship no `npm-shrinkwrap.json`, so its
first-party dependencies float to the newest matching release. tmux stays on
`3.7c`: mise installs it from `tmux/tmux-builds`, which has no `3.8` build.
Rust `1.99.1`, sem `0.28.x`, Pi `1.3.0`, and gh `2.103.0` are younger than seven
days and fix no disclosed vulnerability. ripgrep, fd, and inspect have no newer release.

Reviewed 2026-10-30 (cutoff 2026-10-23): mise moves to `2026.10.24`,
gh to `2.103.3`, sem to `0.29.0`, and Rust to `1.99.1`. mise, gh, and sem are
younger than seven days; the owner approved them as security exceptions. mise
`2026.10.19` makes pinned-checksum, Node-signature, SLSA, and attestation checks
fail closed and sends the GitHub token only to GitHub API hosts; `2026.10.22`
rejects git refs that inject options; `2026.10.23` stops projects from granting
themselves personal fnox secrets. gh `2.103.1` updates `digitorus/pkcs7` for
`GHSA-3xqc-rpq8-wgp9`. sem `0.28.2` updates `webpki-roots` to `1.0.10`, which
drops the compromised eMudhra CAs. Rust `1.99.1` passes the age gate and fixes
a miscompilation. Every new mise, gh, and sem archive matched its published
checksum file. Since mise `2026.10.19`, `lockfile_mode = "generate"` is the
default, Node installs need `SHASUMS256.txt.sig`, and `mise set`/`unset` ask
for trust. Bootstrap writes no lockfile, installs Node from `nodejs.org`, which
publishes the signature, and edits only global config with `mise config set`.
Node.js, npm, uv, tmux, ripgrep, fd, ast-grep, inspect, Herdr, and Pi keep
their pins.

| Updated tool | Selected release | Published (UTC) | Selection |
| --- | --- | --- | --- |
| Tailscale | 1.104.4 | 2026-10-28 01:05 | Approved security exception; Tailscale SSH and Serve fixes (TS-2026-014 to TS-2026-017) |
| Docker Engine / CLI | 29.9.2 | 2026-10-20 20:51:17 | Approved security exception; Go `CVE-2026-111943` rebuild |
| containerd | 2.4.2 | 2026-10-13 22:35:06 | Normal age gate; Docker's Noble package line is now 2.4 |
| Buildx | 0.38.1 | 2026-10-20 22:09:11 | Approved security exception; Go `CVE-2026-111943` rebuild; reviewed official binary fallback |
| Compose | 5.6.1 | 2026-10-21 07:54:18 | Approved security exception; Go `CVE-2026-111943` rebuild |
| mise | 2026.10.24 | 2026-10-30 10:15:02 | Approved security exception; fail-closed verification, GitHub token limited to GitHub hosts, git ref option-injection and fnox secret-grant fixes |
| Node.js | 24.22.1 LTS | 2026-10-20 | Approved security exception; Node 24 security release |
| Pi | 0.87.1 | 2026-09-22 | Normal age gate |
| Pi dependency `undici` | 8.12.3 | 2026-10-19 14:02:56 | Normal age gate; local repair of Pi's shrinkwrapped `8.10.2` (TLS validation bypass, cache poisoning) |
| uv | 0.13.5 | 2026-10-22 01:35:54 | Approved security exception; removes the compromised emSign root (`GHSA-hq85-5cpj-mr62`) |
| Rust | 1.99.1 | 2026-10-16 04:47:44 | Normal age gate; miscompilation fix |
| gh | 2.103.3 | 2026-10-26 19:20:08 | Approved security exception; `digitorus/pkcs7` fix for `GHSA-3xqc-rpq8-wgp9` |
| ast-grep | 0.51.0 | 2026-10-15 15:10:15 | Normal age gate; fixes `GHSA-xx6h-xcfw-vf5j` and `GHSA-cmvf-x8f7-rvp6` |
| sem | 0.29.0 | 2026-10-30 00:51:43 | Approved security exception; `webpki-roots` `1.0.10` drops compromised eMudhra CAs |
| Herdr | 0.9.5 | 2026-10-14 16:07:28 | Normal age gate |
| cloudflared | 2026.10.2 | 2026-10-20 09:17:30 | Normal age gate; Go `1.27.3` build distrusts compromised eMudhra intermediate CAs |

Primary selection and security evidence:

- https://nodejs.org/dist/index.json
- https://registry.npmjs.org/@earendil-works%2Fpi-coding-agent
- https://api.launchpad.net/1.0/ubuntu/+archive/primary
- https://download.docker.com/linux/ubuntu/dists/noble/stable/binary-amd64/Packages.gz
- https://download.docker.com/linux/ubuntu/dists/noble/stable/binary-arm64/Packages.gz
- https://github.com/moby/moby/releases/tag/docker-v29.9.2
- https://github.com/containerd/containerd/releases/tag/v2.4.2
- https://github.com/docker/compose/releases/tag/v5.6.1
- https://go.dev/issue/82345
- https://github.com/containerd/containerd/security/advisories/GHSA-vwhp-8p66-mq4f
- https://github.com/containerd/containerd/security/advisories/GHSA-8647-2jxj-p2rc
- https://github.com/containerd/containerd/security/advisories/GHSA-hc94-p972-ch66
- https://github.com/containerd/containerd/security/advisories/GHSA-pg57-6jwg-q645
- https://github.com/docker/buildx/releases/tag/v0.38.1
- https://github.com/docker/buildx/security/advisories/GHSA-gwr2-q96m-6682
- https://github.com/docker/buildx/security/advisories/GHSA-p54p-jq4x-rc28
- https://github.com/jdx/mise/security/advisories/GHSA-333c-h2jr-xv83
- https://github.com/jdx/mise/security/advisories/GHSA-wcqh-j26q-g44x
- https://github.com/astral-sh/uv/releases/tag/0.13.5
- https://github.com/astral-sh/uv/security/advisories/GHSA-hq85-5cpj-mr62
- https://github.com/cli/cli/releases/tag/v2.103.3
- https://github.com/cli/cli/pull/14791
- https://nodejs.org/dist/index.json
- https://github.com/ast-grep/ast-grep/security/advisories/GHSA-xx6h-xcfw-vf5j
- https://github.com/ast-grep/ast-grep/security/advisories/GHSA-cmvf-x8f7-rvp6
- https://github.com/astral-sh/uv/security/advisories/GHSA-2cv4-cqwr-gwf7
- https://github.com/jdx/mise/releases/tag/v2026.10.24
- https://github.com/jdx/mise/releases/tag/v2026.10.19
- https://github.com/jdx/mise/pull/14781
- https://github.com/jdx/mise/releases/download/v2026.10.24/SHASUMS256.txt
- https://github.com/Ataraxy-Labs/sem/releases/download/v0.29.0/checksums.txt
- https://github.com/cli/cli/releases/download/v2.103.3/gh_2.103.3_checksums.txt

On 2026-10-22 the Docker repository contains Engine 29.9.2, containerd 2.4.2,
Buildx 0.38.1, and Compose 5.6.1 on both architectures. Keep the reviewed
official binary for hosts whose apt candidate cannot satisfy the Buildx floor. Both Linux binaries were independently
downloaded and matched `checksums.txt` and GitHub release asset digests.

- amd64 SHA-256: `e25d5cd675092bb5d1d03632fdc46cdf3fede7b514dc5f023a105e00d3ce5ad4`
- arm64 SHA-256: `3b83b3731916ba433feccaaa689fe5bcdc81c641d92c8e6c2ea865dfa141c97a`
- https://github.com/docker/buildx/releases/tag/v0.38.1
- https://github.com/docker/buildx/releases/download/v0.38.1/checksums.txt
- https://github.com/docker/buildx#manual-download
- https://github.com/docker/cli/blob/master/cli-plugins/manager/manager.go

Docker documents system-wide manual plugin installation but discourages
unmanaged production downloads because automatic security updates are absent.
ServerPro therefore owns fallback integrity, replacement, and return to apt.
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

The 2026-10-30 review found that Pi `0.87.1` and `1.0.0` also pin
`undici@8.10.2` exactly. Pi requires it directly, and the shrinkwrap holds one
hoisted copy. Eight advisories affect `>=8.0.0 <8.12.2`, including a TLS
certificate validation bypass and cross-origin cache poisoning. Version
`8.12.3` was published 2026-10-19 14:02 UTC and passes the normal seven-day
age gate. It has no dependencies, requires Node.js `>=22.19.0`, and its
registry SHA-512 matches the pinned integrity. With both repairs applied, the
npm bulk advisory endpoint reported no advisories for the 138 runtime
packages in the Pi `0.87.1` shrinkwrap. No real Pi install was run for this review.

- https://github.com/advisories/GHSA-2m5v-pch6-74hx
- https://github.com/advisories/GHSA-4w7w-5xrc-9h26
- https://github.com/advisories/GHSA-xjj6-f33q-929c
- https://github.com/advisories/GHSA-qrxm-fqhp-jwvm
- https://github.com/advisories/GHSA-4qv8-2p9j-2685
- https://github.com/advisories/GHSA-f72v-6w45-q472
- https://github.com/advisories/GHSA-9fm3-97px-rjf6
- https://github.com/advisories/GHSA-55wc-3pg8-jfw6
- https://registry.npmjs.org/undici
- https://registry.npmjs.org/@earendil-works/pi-coding-agent/-/pi-coding-agent-0.87.1.tgz
- https://registry.npmjs.org/@earendil-works/pi-coding-agent/-/pi-coding-agent-1.0.0.tgz

These findings are a point-in-time review, not proof that all dependencies or
artifacts are safe. The local Pi repair passed its runtime dependency audit.
A safe Buildx apt package or the reviewed official binary is required.
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
https://pkg.go.dev/vuln/GO-2026-6611
https://pkg.go.dev/vuln/GO-2026-6612
https://pkg.go.dev/vuln/GO-2026-6613
https://pkg.go.dev/vuln/GO-2026-6617
https://pkg.go.dev/vuln/GO-2026-6603
https://pkg.go.dev/vuln/GO-2026-6605
https://pkg.go.dev/vuln/GO-2026-6607
https://pkg.go.dev/vuln/GO-2026-6608
https://pkg.go.dev/vuln/GO-2026-6610
https://vuln.go.dev/ID/GO-2026-6603.json
https://pkg.go.dev/vuln/GO-2026-6809
```

Use: minimum supported Go version and official vulnerability evidence. Reviewed
2026-08-11: Go 1.26.5 fixes `crypto/tls` and `os` security issues affecting Go
1.26.0 through 1.26.4. Reviewed 2026-08-26: Go 1.26.6 (released 2026-08-13,
announced on golang-announce and oss-security) fixes ten security issues,
including the four standard-library advisories govulncheck reported reachable
from serverpro under Go 1.26.5 (`net/url`, `crypto/tls`, `encoding/asn1`, and
the vendored `x/net/idna` path). Reviewed 2026-10-11: Go 1.26.9 (released
2026-10-08) includes security fixes to `net/http` and other packages, clearing
the four `net/http` advisories govulncheck reported reachable from serverpro
under Go 1.26.6 (GO-2026-6611, GO-2026-6612, GO-2026-6613, GO-2026-6617). The
`go` directive is the mandatory minimum toolchain version, so source builds
require Go 1.26.9 or newer. Reviewed 2026-10-11 (second pass): govulncheck
under a local Go 1.27.1 reported eight reachable advisories (GO-2026-6603,
GO-2026-6605, GO-2026-6607, GO-2026-6608, GO-2026-6610, GO-2026-6611,
GO-2026-6613, GO-2026-6617). The official vulnerability database records
each as fixed in Go 1.26.9 and Go 1.27.2, both released 2026-10-08 with the
same security fix set; none is withdrawn. The 1.26.9 floor stays. Go 1.27.0
and 1.27.1 are newer than the floor but unpatched, and `GOTOOLCHAIN=auto`
never switches down, so a local 1.27.0 or 1.27.1 must not run project gates.
Reviewed 2026-10-21: govulncheck reported GO-2026-6809 (`crypto/x509`
denylist for distrusted intermediate certificates) reachable under Go 1.27.2.
The official vulnerability database records it as fixed in Go 1.26.10 and Go
1.27.3 (released 2026-10-19). Go 1.27.3 is newer than the usual release-age
window, and the owner approved this security exception, so the pin moves to
Go 1.27.3 and a local 1.27.2 must not run project gates.

### Go quality tools

```text
https://github.com/golangci/golangci-lint/releases/tag/v2.14.0
https://api.github.com/repos/golangci/golangci-lint/releases/tags/v2.14.0
https://golangci-lint.run/docs/product/migration-guide/
https://github.com/golang/vuln/tree/v1.6.0
https://api.github.com/repos/golang/vuln/git/ref/tags/v1.6.0
```

Use: project-local lint and vulnerability scanner pins. Reviewed 2026-08-11:
golangci-lint `v2.14.0` (bumped 2026-10-13 for Go 1.27 export data) is an immutable stable release; govulncheck `v1.6.0`
is an exact official repository tag. Both require Go 1.25 or newer and support
the Go 1.27.3 project toolchain.

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
https://github.com/tailscale/tailscale/releases/tag/v1.104.4
https://tailscale.com/security-bulletins
https://pkgs.tailscale.com/stable/tailscale_1.104.4_amd64.tgz.sha256
https://pkgs.tailscale.com/stable/tailscale_1.104.4_arm64.tgz.sha256
https://github.com/tailscale/tailscale/issues/20067
```

Use: devices, keys, policy read, policy validate, policy update, and pinned
first-boot/live-repair binaries. Reviewed 2026-10-30: stable release `1.104.4`,
published 2026-10-28 01:05 UTC, is an approved security exception. It fixes
Tailscale SSH unix-socket forwarding to root-only sockets (TS-2026-014), an SSH
memory denial of service (TS-2026-015), and Serve flaws (TS-2026-016,
TS-2026-017). It keeps the TS-2026-013 eMudhra distrust that needs `1.104.2` or
later. Hosts already run `1.104.2`, so the older exact pin would downgrade them.
amd64 tarball SHA-256
`624ea093d47d62cf56f17b8332d74b8decda4a1faa610ebc60a1de334b2b3dc9`;
arm64 tarball SHA-256
`a1c249ce6bdebef62e1ae3b997595555bce2ae4709bf95a718e9ca7a129dad7e`.
Serverpro supplies `GODEBUG=tlsmlkem=1` to the systemd service independently
from the artifact build default. Recheck the stable release, checksums,
advisory state, and binary build setting together when rotating this pin.

### Tailscale SSH

```text
https://tailscale.com/kb/1193/tailscale-ssh
```

Use: mandatory admin path and SSH policy behavior.

### Tailscale policy file and grants

```text
https://tailscale.com/docs/reference/syntax/policy-file
https://tailscale.com/docs/reference/syntax/grants
https://tailscale.com/docs/features/access-control/acls
https://tailscale.com/docs/reference/examples/acls
```

Use: doctor `tailnet policy` allow-all detection and the recommended grants
example in `docs/tailscale-setup.md`. Reviewed 2026-10-30: a file with no
`acls` section gets the default allow-all policy; `"acls": []` denies all;
a grant `ip` of `tcp:22` limits it to TCP 22, while a bare port also opens UDP
and ICMP; Tailscale SSH needs a network rule to port 22 and an `ssh` rule.
The pages do not say whether the default allow-all still applies to a file
with `grants` and no `acls`. `tests` cover grants and ACLs, take a user,
group, or tag as `src` and `tag:name:port` destinations, and a failing test
makes Tailscale reject the policy change; `autogroup:self` applies to a
member's own untagged devices only.

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
https://github.com/cloudflare/cloudflared/releases/tag/2026.10.2
https://raw.githubusercontent.com/cloudflare/cloudflared/master/RELEASE_NOTES
https://blog.cloudflare.com/emudhra-certificate-compromise/
```

Use: connector install source, Ubuntu 24.04 package floor `2026.10.2`, and apt
trust root. Package indexes and release reviewed 2026-10-30. Earlier builds
still trust eight eMudhra intermediate CAs whose private keys were compromised;
Cloudflare's 2026-10-20 notice requires `2026.10.2` or later. The noble index
then offered only `2026.10.6` (published 2026-10-29), so install and repair take
that build while the reviewed floor stays at the age-gated `2026.10.2`. Primary
package-signing key fingerprint retrieved 2026-07-23 and re-verified 2026-10-30:
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
https://ubuntu.com/security/notices/USN-8932-1
https://ubuntu.com/security/notices/USN-8928-1
https://ubuntu.com/security/notices/USN-8944-1
https://bugs.launchpad.net/bugs/2171054
```

Use: Ubuntu 24.04 LTS (`noble`) managed-host support, host hardening, and direct
package minimums. Reviewed 2026-10-30 for amd64 and arm64: `ca-certificates`
`20260816ubuntu0.24.04.1`, `curl` `8.5.0-2ubuntu10.16`, `gnupg`
`2.4.4-2ubuntu17.6`, `ufw` `0.36.2-6`, `apparmor`
`4.0.1really4.0.1-0ubuntu0.24.04.8`, `unattended-upgrades`
`2.9.1+nmu4ubuntu1`, `jq` `1.7.1-3ubuntu0.24.04.2`, `git`
`1:2.43.0-1ubuntu7.4`, `openssh-client` `1:9.6p1-3ubuntu13.20`, and `htop`
`3.3.0-4build1`. These are reviewed floors; signed newer candidates remain
valid and must not be downgraded.
The 2026-10-30 review raises four floors to security releases that already
pass the seven-day age gate. curl `8.5.0-2ubuntu10.16` (USN-8932-1) and
openssh-client `1:9.6p1-3ubuntu13.20` (USN-8928-1) were published
2026-10-14. git `1:2.43.0-1ubuntu7.4`, published 2026-10-19, fixes the
regression that curl update causes (LP#2171054), so both floors move together.
ca-certificates `20260816ubuntu0.24.04.1` (USN-8944-1, 2026-10-20) removes
emSign ECC Root CA - G3 (Mozilla 2.90). Its version format changed, but dpkg
ordering still sorts it above `20260601~24.04.1`: the leading date
`20260816` is greater than `20260601`. apparmor
`4.0.1really4.0.1-0ubuntu0.24.04.9` exists only in `noble-updates` with no
security fix, so its floor stays. `dpkg-query -W` can retain version data for a
removed package in `config-files` state, so package-floor checks require
`${db:Status-Status}` to be exactly `installed` before trusting `${Version}`.

### UFW

```text
https://documentation.ubuntu.com/server/how-to/security/firewalls/
https://tailscale.com/docs/reference/faq/firewall-ports
```

Use: host firewall checks and the restricted egress lockdown. Tailscale's
firewall FAQ states that the coordination server and DERP relays use HTTPS on
443, so the lockdown proves `controlplane.tailscale.com:443` still connects.

### OpenSSH

```text
https://documentation.ubuntu.com/server/how-to/security/openssh-server/
```

Use: public, root, and password SSH hardening.

### sshd and sshd_config

```text
https://manpages.ubuntu.com/manpages/noble/en/man8/sshd.8.html
https://manpages.ubuntu.com/manpages/noble/en/man5/sshd_config.5.html
```

Use: `sshd -t` test mode and `-f` for validating a candidate config before the
doctor repair commits it, and `Include` glob semantics (files are read in
lexical order and the first value obtained wins). Checked against OpenSSH
9.6p1 (Ubuntu 24.04) on 2026-10-11.

### apt preferences and lock waiting

```text
https://manpages.ubuntu.com/manpages/noble/en/man5/apt_preferences.5.html
https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=864681
https://bugs.launchpad.net/cloud-init/+bug/1693361
https://salsa.debian.org/apt-team/apt/-/blob/2.8.3/apt-pkg/deb/debsystem.cc
https://salsa.debian.org/apt-team/apt/-/blob/2.8.3/apt-pkg/acquire.cc
```

Use: `Pin: origin "pkg.cloudflare.com"` for the cloudflared pin (priorities
above 500 win over default repositories), and `-o DPkg::Lock::Timeout`, which
apt-get honours since apt 1.9.11 (Debian bug 864681) to wait for the dpkg lock
instead of failing while unattended-upgrades runs. In apt 2.8.3 (noble),
`debSystem::Lock` applies that timeout only to `/var/lib/dpkg/lock-frontend`
and `/var/lib/dpkg/lock`; `pkgAcquire::GetLock` takes
`/var/lib/apt/lists/lock` with a single attempt. apt-daily holds that lists
lock while it refreshes indexes (Launchpad bug 1693361), so `apt-get update`
fails at once with "Could not get lock /var/lib/apt/lists/lock" and the host
scripts retry only that C-locale error within the same budget. Checked against
apt 2.8.3 on Ubuntu 24.04 on 2026-10-11, including a container run where
`apt-get update` succeeded while another process held the dpkg frontend lock.

### sudo

```text
https://www.sudo.ws/docs/man/sudo.man/
```

Use: password-aware sudo execution.

### sudoers

```text
https://www.sudo.ws/docs/man/sudoers.man/
```

Use: `NOPASSWD` and `PASSWD` semantics, and `verifypw=any` for the first-boot
grant (with the default `verifypw=all`, the admin's `%sudo` entry makes
`sudo -v` demand the not-yet-set password).

### chpasswd

```text
https://manpages.ubuntu.com/manpages/noble/man8/chpasswd.8.html
```

Use: without `--encrypted`, chpasswd hashes the stdin password through PAM;
Ubuntu 24.04's `common-password` uses `pam_unix.so obscure yescrypt`, so the
admin password is stored as `$y$`.

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

### Provider metadata user-data

```text
https://docs.digitalocean.com/reference/api/metadata/droplet-properties/
https://www.vultr.com/docs/manage-instance-user-data-with-the-vultr-metadata-api
https://github.com/coreos/ignition/commit/ef142f33
```

Use: the metadata user-data path each compute adapter declares for the doctor
`metadata user-data` check: DigitalOcean `/metadata/v1/user-data`, Vultr
`/latest/user-data`, Hetzner `/hetzner/v1/userdata`, all on
`http://169.254.169.254`. Vultr serves the previous user-data until the
instance reboots after an update. Reviewed 2026-10-30; the DigitalOcean and
Vultr paths were also read live on managed hosts. No Hetzner host was
available, so its path rests on the Ignition provider source.

### OpenSSH off and provider consoles

```text
https://tailscale.com/docs/features/tailscale-ssh
https://docs.digitalocean.com/products/droplets/how-to/connect-with-console
https://docs.digitalocean.com/products/droplets/how-to/recovery/recovery-console/
https://docs.vultr.com/products/compute/cloud-compute/connection/vultr-console
```

Use: the OpenSSH-off default and its recovery notes. Tailscale SSH serves
SSH, including SFTP, from `tailscaled` without sshd. The DigitalOcean Droplet
Console needs the droplet agent and a running sshd; the Recovery Console does
not. The Vultr View Console logs in on the server console with the user's
password. The `sshd_not_to_be_run` condition comes from the `ssh.socket` and
`ssh.service` units in `openssh-server` `1:9.6p1-3ubuntu13.20` (noble),
checked in a container. Reviewed 2026-10-30.

### Unattended-upgrades options

```text
https://documentation.ubuntu.com/server/how-to/software/automatic-updates/
https://github.com/mvo5/unattended-upgrades/blob/master/README.md
https://docs.vultr.com/products/compute/cloud-compute/management/restart-instance
```

Use: the serverpro unattended-upgrades drop-in and the reboot advice. APT
reads apt.conf.d in lexical order, and the upstream README asks for a file
that sorts after `50unattended-upgrades` instead of edits to it. In
`2.9.1+nmu4ubuntu1` (noble), `Remove-Unused-Kernel-Packages` defaults to true
in code but is commented out in the shipped file, and `Automatic-Reboot`
defaults to false. Vultr documents an instance restart as a hard reboot, so
doctor recommends a graceful host reboot. Reviewed 2026-10-30.

### Gated reboot

```text
https://docs.docker.com/engine/containers/start-containers-automatically/
https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html
https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html
https://www.freedesktop.org/software/systemd/man/latest/loginctl.html
```

Use: the gated reboot's rules and units. Docker restarts `always` and
`unless-stopped` containers when the daemon starts, and documents that
`on-failure` does not restart a container when the daemon restarts. With
Docker 29.1.3 an `on-failure` container killed at shutdown came back and one
that exited 0 did not, so the gate accepts only `always` and
`unless-stopped`. `Persistent=false` keeps a timer from running a missed
window at boot. `Type=exec` lets boot finish while the verify unit waits,
and `RuntimeMaxSec=` stops it if its own deadlines fail. A lingering user's
manager starts at boot; without linger it stops at the last logout.
Reviewed 2026-10-30, and checked in a systemd 255 container.

### Provider image accounts

```text
https://docs.vultr.com/support/platform/ssh-keys/how-do-i-use-an-ssh-key-with-a-non-root-user-on-a-vultr-instance
https://docs.digitalocean.com/products/droplets/getting-started/recommended-droplet-setup/
https://community.hetzner.com/tutorials/basic-cloud-config
```

Use: the image accounts each compute adapter declares for removal. Vultr's
limited login account is `linuxuser` with sudo; managed Vultr hosts also had
it. DigitalOcean and Hetzner Ubuntu images log in as root only, so their
adapters declare none. Reviewed 2026-10-30.

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
`9DC858229FC7DD38854AE2D88D81803C0EBFCD88`. Reviewed 2026-10-22 floors for
Ubuntu 24.04 amd64/arm64: Docker Engine and CLI
`5:29.9.2-1~ubuntu.24.04~noble`, containerd.io
`2.4.2-3~ubuntu.24.04~noble`, Buildx
`0.38.1-1~ubuntu.24.04~noble`, and Compose
`5.6.1-1~ubuntu.24.04~noble`.

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
https://github.com/jdx/mise/releases/tag/v2026.10.24
https://api.github.com/repos/jdx/mise/releases/tags/v2026.10.24
https://github.com/jdx/mise/releases/download/v2026.10.24/SHASUMS256.txt
https://github.com/jdx/mise/releases/tag/v2026.10.19
https://github.com/jdx/mise/releases/tag/v2026.10.22
https://github.com/jdx/mise/releases/tag/v2026.10.23
```

Use: mise prerequisite, scoped managed-tool installation, legacy managed-tool
removal, and reviewed minimum version `2026.10.24`. Pinned release artifact
SHA-256 values, matched against the release `SHASUMS256.txt` on 2026-10-30:

- Linux x64: `9a6c9c55f5c6372d0af70a9979fe6d742aea3793267a929e1738022143c466cc`
- Linux arm64: `8c8f5d7f748bf3aef431dc50f0ad776270f36025c788aa21c74e2fabdbbd6d3e`

The `2026.10.24` floor is an owner-approved exception to the seven-day release
age gate. It keeps the `2026.10.17` eMudhra distrust and adds fail-closed
verification, GitHub-host-only token use, git ref option-injection rejection,
and the fnox personal-secret fix. Its breaking changes were checked against
bootstrap: no lockfile is enabled, Node comes from `nodejs.org` with its
`SHASUMS256.txt.sig`, and only global config is written with `mise config set`.

### mise bootstrap

```text
https://mise.jdx.dev/bootstrap.html
https://mise.jdx.dev/cli/bootstrap.html
```

Use: managed package convergence through `mise bootstrap packages apply` and
scoped managed-tool installs through `mise install`.

### Node.js

```text
https://nodejs.org/en/blog/release/v24.22.1
https://nodejs.org/dist/v24.22.1/SHASUMS256.txt
https://nodejs.org/en/about/previous-releases
https://nodejs.org/en/blog/vulnerability/july-2026-security-releases/
```

Use: pinned Node `24.22.1` LTS security release and bundled npm `11.19.1`;
reviewed 2026-10-22 on the supported Node 24 LTS line. Published GNU/Linux
SHA-256 values: x64 `6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6`;
arm64 `dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e` (`.tar.xz`).

### uv

```text
https://github.com/astral-sh/uv/releases/tag/0.13.5
https://github.com/astral-sh/uv/security/advisories/GHSA-hq85-5cpj-mr62
https://docs.astral.sh/uv/getting-started/installation/
```

Use: pinned uv `0.13.5` through mise's explicit `aqua:astral-sh/uv` backend;
reviewed 2026-10-22. Published SHA-256 files and GitHub asset digests agree:
x86_64 `f68d291c75783122dad58ab77def0e13f3233f358731db5d6d7e8715603b928f`;
aarch64 `4bb69cf95ed13f23713bb7bc09f241896ea0c75b7c557e615a20604b82f9c403`.

### Rust and rustup

```text
https://github.com/rust-lang/rust/releases/tag/1.99.1
https://doc.rust-lang.org/stable/releases.html
https://mise.jdx.dev/lang/rust.html
https://rust-lang.github.io/rustup/security.html
```

Use: pinned Rust `1.99.1` through mise's `core:rust` backend and rustup default
profile. Reviewed 2026-10-30. rustup uses HTTPS for downloads but does not yet
enforce download signatures.

### mise npm backend

```text
https://mise.jdx.dev/dev-tools/backends/npm.html
```

Use: reference for npm-backed mise tools. serverpro does NOT use the `npm:`
backend; it installs Pi `0.87.1` via `npm install -g` under mise-managed Node
`24.22.1` with lifecycle-script suppression
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
https://github.com/herdrdev/herdr/releases/tag/v0.9.5
https://api.github.com/repos/herdrdev/herdr/releases/tags/v0.9.5
```

Use: managed Herdr `0.9.5` installation through mise's explicit GitHub backend,
package-manager update ownership, and target-user Pi integration status.
Reviewed 2026-10-22. Pinned release SHA-256 values:

- Linux x64: `14d9e8bc8b4a9e0118d77c32266f76d461f09b40342eb23752960bd89f59929d`
- Linux arm64: `17d7536f2da50859523680ef279dffcac8e4a235f4e46fe8da6c2ec44141897c`

### GitHub CLI

```text
https://cli.github.com/manual/
https://github.com/cli/cli/releases/tag/v2.103.3
https://github.com/cli/cli/pull/14791
```

Use: pinned `gh` `2.103.3` install reference; reviewed 2026-10-30.
Published GNU/Linux SHA-256 values: amd64
`4c806978ffce1cec70853302a56d70ac6da4f19bae7adca4e1a8ab00633dd91d`; arm64
`ee32df2323c8847f5fac6cb6b4b69ca75828e9329a8f22f677021f7b3306ce63` (`.tar.gz`).
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
https://github.com/ast-grep/ast-grep/releases/tag/0.51.0
https://api.github.com/repos/ast-grep/ast-grep/releases/tags/0.51.0
https://github.com/ast-grep/ast-grep/security/advisories/GHSA-xx6h-xcfw-vf5j
https://github.com/ast-grep/ast-grep/security/advisories/GHSA-cmvf-x8f7-rvp6
```

Use: pinned ast-grep `0.51.0` through mise's GitHub backend; reviewed
2026-10-22. Pinned release SHA-256 values:

- Linux x64: `a7eead5552d3a8daad70f5d50d935da44bd0c138e946b5b0a96b1d38169fa639`
- Linux arm64: `73af0ebe5958acee012754a5b2826a186580ef83333092408a7a01096fc30b8b`

### sem

```text
https://github.com/Ataraxy-Labs/sem
https://github.com/Ataraxy-Labs/sem/releases/tag/v0.29.0
https://api.github.com/repos/Ataraxy-Labs/sem/releases/tags/v0.29.0
https://github.com/Ataraxy-Labs/sem/pull/526
```

Use: pinned Ataraxy Labs sem `0.29.0` through mise's GitHub backend; reviewed
2026-10-30. Pinned release archive (`sem-linux-*.tar.gz`) SHA-256 values:

- Linux x64: `30fa7ab79aaffaf0cc2d317ac771fa4330e6de6a9c0830cbe35d1ba149322dc3`
- Linux arm64: `e7cd85d2c34dc6e8a9505a365a8639b7203c8cf6ad2d94e2b8a07e1acc8854b5`

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
https://github.com/actions/upload-artifact/releases/tag/v7.0.2
https://github.com/actions/download-artifact/releases/tag/v8.0.2
https://github.com/actions/attest-build-provenance/releases/tag/v4.2.2
https://github.com/actions/attest/releases/tag/v4.2.2
https://github.com/anchore/sbom-action/releases/tag/v0.24.3
https://github.com/anchore/syft/releases/tag/v1.48.0
```

Use: release workflow, native runner architecture, artifact transport, SPDX
SBOM, signed provenance, attached-bundle verification, and release-integrity
guidance. Reviewed 2026-08-11: checkout is pinned to `v7.0.1` and setup-go to
`v7.0.0`; both declare the supported Node 24 JavaScript action runtime, and the
release runner uses Go `1.27.3`. Upload/download/attestation actions are pinned
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
