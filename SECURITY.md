# Security

Treat every non-dry-run command as live infrastructure work.

## Supported versions

Until `v1.0.0`, only the latest release and `main` receive security fixes.
Older tags are best-effort.

## Report a vulnerability

Use GitHub Private Vulnerability Reporting for this repository when enabled.
For public GitHub repositories, maintainers can enable it in repository
`Settings` → `Advanced Security` → `Private vulnerability reporting`.

Include:

- affected version or commit
- provider and command path
- reproduction steps
- impact and reachable secret/resource scope
- proof of concept when safe

Do not open public issues with exploit details, tokens, hostnames, public IPs,
or recovery credentials. If private reporting is unavailable, open a minimal
public issue asking maintainers to enable a private reporting channel, without
sensitive details.

Expected handling: maintainers acknowledge, reproduce, patch privately when
needed, publish a fixed release, then disclose enough detail for users to act.

## Access model

serverpro assumes hostile public networks and keeps administration private by
default.

- Tailscale SSH is mandatory for normal administration.
- Public SSH is disabled.
- Public app ingress defaults to `none`.
- Current selectable ingress modes are `none` and `cloudflare-tunnel`.
- Cloudflare Tunnel support is connector/local-route metadata only; routes are
  not made publicly routable yet.
- Privileged remote actions require the remote admin sudo password.
- During first server creation, users choose the remote admin sudo password and
  must save it for later serverpro operations.
- Users should change or disable default provider root credentials after first
  access, according to provider recovery and console guidance.
- Unrestricted `NOPASSWD:ALL` sudo is a failing managed-state condition.

## Credentials and secrets

Provider and service credentials live in server-scoped files:

```text
~/.config/serverpro/namespaces/<namespace>/servers/<server>/credentials.json
```

Credential directories must be `0700`; files must be `0600`.

This is the early-release credential model. Future hardening should evaluate OS
keychains, encrypted local databases, explicit lock/unlock flows, and
memory-only prompts where credentials are requested for each operation and then
discarded.

Never expose provider tokens, tunnel tokens, Tailscale auth keys, sudo
passwords, password hashes, or cloud-init bootstrap data in logs, state, JSON,
errors, docs, plans, tests, or issue reports. Remote admin sudo passwords are
runtime-only. Direct user-supplied Tailscale auth keys are rejected; serverpro
creates short-lived tagged keys.

## Supply-chain verification

Managed host tools install through integrity-checked sources, not
`curl ... | sh`:

- Tailscale is added via its official Ubuntu 24.04 apt repository after its
  package key matches fingerprint
  `2596A99EAAB33821893C0A79458CA832957F5868`; no network-fetched shell script
  is executed.
- Cloudflared is added via its official apt repository after its current
  package key matches fingerprint
  `CC94B39C77AE7342A68B89628A682D308D4E5E73`.
- Docker is added via its official apt repository, pinned to the published GPG
  key fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`.
- mise is fetched as a release tarball and verified against its published
  SHA-256 checksum before the target user installs it into `~/.local/bin`.
- Herdr is installed for the target user through mise's explicit GitHub backend
  at a pinned version; bootstrap and doctor verify the resulting Linux binary
  against the architecture-specific SHA-256 digest published with that GitHub
  release. Bootstrap installs the bundled Pi integration under the target user's
  private Pi agent directory and does not invoke Herdr self-update or session
  lifecycle commands.
- The bootstrap script runs as root only for system-package and apt work;
  per-user tool installation runs as the target user, and root does not write
  into user home paths.

## Release artifact controls

Release builds use exact-version Go, `-trimpath`, and an explicit archive
allowlist. Every archive carries project and dependency license/notice material.
Packaging rejects unexpected members and binary strings matching developer home
paths, private URLs, or high-specificity credential forms. Checksums and a
source-bound release manifest are published beside archives. Release automation
never replaces existing assets; corrections require a new tag and release.

These controls cover official release archives, not ignored local binaries,
tool caches, or VCS metadata. Publish only workflow-produced `dist` assets.

## Safe operations

Power and delete operations require:

- local state-known provider IDs
- provider identity and namespace/server ownership metadata
- confirmation gates

State is removed only after every tracked compute, access-policy, and external
resource cleanup succeeds. A failed create that never reached compute skips the
absent compute deletion but still cleans checkpointed external resources.
Partial failures retain state and report remaining resources. Local state can
drift from provider reality.

`serverpro server discover` lists compute resources labeled
`managed-by=serverpro` on a provider token. `serverpro server import` rebuilds
local config, credentials, state, and registry from those labels after the
operator re-supplies tokens. Optional `--with-tailscale` and `--with-cloudflare`
reattach mesh/tunnel metadata when those APIs are reachable. Recovery requires
complete provider metadata plus a unique owned access policy and fails before
writes on unsupported existing state or registry schemas. Import never stores
tokens in state JSON; credentials remain server-scoped private files. A future
`serverpro server sync` command should refresh existing local entries without
deleting cloud resources unless an explicit delete command is used.

## Emergency recovery

Use provider recovery tools when Tailscale SSH is unavailable:

- Hetzner: Cloud Console VNC or Rescue System.
- DigitalOcean: Recovery Console or Recovery ISO.
- Vultr: web console or rescue ISO.

After recovery, run `serverpro server doctor` and verify public SSH is still
closed.

## Manual public SSH checks

```sh
nc -vz -w5 <public-ip> 22
nmap -Pn -p22 <public-ip>
```

A successful public SSH connection means the target is not in the expected
secure state.

## Scope boundaries

serverpro prepares hardened hosts and safe access paths. It does not deploy
apps, manage databases, store app secrets, create DNS records, or publish app
routes. App repositories own deployment, DNS, runtime configuration, and app
incident response.
