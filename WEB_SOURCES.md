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

| Updated tool | Selected release | Published (UTC) | Selection |
| --- | --- | --- | --- |
| Tailscale | 1.102.4 | 2026-09-10 | Normal age gate |
| Docker Engine / CLI | 29.8.2 | 2026-09-30 | Approved security exception |
| containerd | 2.3.6 | 2026-09-24 23:13:08 | Normal age gate; preserve Docker's 2.3 package line |
| Buildx | 0.37.2 | 2026-09-30 | Approved security exception; reviewed official binary fallback |
| Compose | 5.5.1 | 2026-09-03 | Normal age gate |
| mise | 2026.10.17 | 2026-10-21 01:29:26 | Approved security exception; distrusts compromised eMudhra intermediate CAs and keeps the inline-options token exfiltration fix |
| Node.js | 24.22.1 LTS | 2026-10-20 | Approved security exception; Node 24 security release |
| Pi | 0.87.1 | 2026-09-22 | Normal age gate |
| uv | 0.13.5 | 2026-10-22 01:35:54 | Approved security exception; removes the compromised emSign root (`GHSA-hq85-5cpj-mr62`) |
| Rust | 1.99.0 | 2026-10-01 12:41:12 | Normal age gate |
| gh | 2.102.1 | 2026-10-20 19:35:51 | Approved security exception; Go `1.27.3` standard-library fixes |
| ast-grep | 0.51.0 | 2026-10-15 15:10:15 | Normal age gate; fixes `GHSA-xx6h-xcfw-vf5j` and `GHSA-cmvf-x8f7-rvp6` |
| sem | 0.27.0 | 2026-10-04 11:22:08 | Normal age gate |
| Herdr | 0.9.5 | 2026-10-14 16:07:28 | Normal age gate |
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
- https://github.com/astral-sh/uv/releases/tag/0.13.5
- https://github.com/astral-sh/uv/security/advisories/GHSA-hq85-5cpj-mr62
- https://github.com/cli/cli/releases/tag/v2.102.1
- https://nodejs.org/dist/index.json
- https://github.com/ast-grep/ast-grep/security/advisories/GHSA-xx6h-xcfw-vf5j
- https://github.com/ast-grep/ast-grep/security/advisories/GHSA-cmvf-x8f7-rvp6
- https://github.com/astral-sh/uv/security/advisories/GHSA-2cv4-cqwr-gwf7
- https://github.com/jdx/mise/releases/tag/v2026.10.17
- https://github.com/jdx/mise/pull/14781
- https://github.com/jdx/mise/releases/download/v2026.10.17/SHASUMS256.txt
- https://github.com/Ataraxy-Labs/sem/releases/download/v0.27.0/checksums.txt

The Docker repository contains Engine 29.8.2 and containerd 2.3.6 on both
architectures, but only Buildx 0.37.1. Keep the patched floor; use the reviewed
official binary when apt cannot satisfy it. Both Linux binaries were independently
downloaded and matched `checksums.txt` and GitHub release asset digests.

- amd64 SHA-256: `982ca20490b45ed1ec8d99795974d3d874a358f75938c9c237305010e6b7e548`
- arm64 SHA-256: `efa38cb7aa7db2dbb9ad049b00b0a9737f66f033626177b5a4e845184ad7ab29`
- https://github.com/docker/buildx/releases/tag/v0.37.2
- https://github.com/docker/buildx/releases/download/v0.37.2/checksums.txt
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
https://github.com/jdx/mise/releases/tag/v2026.10.17
https://api.github.com/repos/jdx/mise/releases/tags/v2026.10.17
https://github.com/jdx/mise/releases/download/v2026.10.17/SHASUMS256.txt
```

Use: mise prerequisite, scoped managed-tool installation, legacy managed-tool
removal, and reviewed minimum version `2026.10.17`. Pinned release artifact
SHA-256 values, matched against the release `SHASUMS256.txt` on 2026-10-22:

- Linux x64: `17565e268c5542d8ce944321e17af81e0b741b65af4aa7e8e010ee77376a4aa3`
- Linux arm64: `cac510e51793ad45e65fe92305bdbe4309c9c60b5907badbc7eba111738ab08f`

The `2026.10.17` floor is an owner-approved exception to the seven-day release
age gate. It removes trust in eight compromised eMudhra intermediate CA
certificates from the npm backend's bundled roots. Its only listed upgrade
note changes relative `find_up` in `env._.file`; serverpro does not use it.

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
https://github.com/rust-lang/rust/releases/tag/1.99.0
https://doc.rust-lang.org/stable/releases.html
https://mise.jdx.dev/lang/rust.html
https://rust-lang.github.io/rustup/security.html
```

Use: pinned Rust `1.99.0` through mise's `core:rust` backend and rustup default
profile. Reviewed 2026-10-22. rustup uses HTTPS for downloads but does not yet
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
https://github.com/cli/cli/releases/tag/v2.102.1
```

Use: pinned `gh` `2.102.1` install reference; reviewed 2026-10-22.
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
https://github.com/Ataraxy-Labs/sem/releases/tag/v0.27.0
https://api.github.com/repos/Ataraxy-Labs/sem/releases/tags/v0.27.0
```

Use: pinned Ataraxy Labs sem `0.27.0` through mise's GitHub backend; reviewed
2026-10-22. Pinned release SHA-256 values:

- Linux x64: `3f588a3c62e2d34ba427d51e754f31977922227e5f99a9c69e05977f16900713`
- Linux arm64: `48875c490af25a114c5e8a36a4187a4c1e49e5e1868c5b9f969725420dd6993c`

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
