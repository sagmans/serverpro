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
- Post-bootstrap egress lockdown is fail-closed: an omitted
  `phase_lockdown_after_bootstrap` value behaves as `true`; only explicit
  `false` disables it. Restricted egress still permits outbound SSH (22/tcp)
  for git-over-SSH workflows; `serverpro server doctor NAME --fix` adds the rule
  to hosts provisioned before it existed.
- GitHub development access is opt-in and least-privilege by default: the
  read-only deploy key flow stays the default; full account-key access requires
  a PAT and grants the server git write to every repo the account can write, so
  pair it with branch protection and signed-commit requirements on important
  repositories. Account-key mode manages one GitHub development profile and
  one stored `gh` token per server. A fine-grained PAT covers exactly one
  resource owner even when `all repositories` is selected; that API boundary
  does not narrow the account SSH key's broader Git access. Multiple GitHub
  usernames and multiple PAT resource-owner profiles are unsupported. Changing
  from deploy-key to account-key removes only the exact managed repository
  rewrite and marked SSH block before local deploy scope is cleared; malformed
  managed blocks fail closed.
- The required full-development GitHub PAT enters only through a masked prompt,
  travels over SSH stdin, and is stored only on the managed host as a `0600` gh
  `hosts.yml`; it never appears in local config, state, logs, or process lists.
  Server-side SSH signing keys live on the host: any process running as the
  admin user can sign commits, an accepted boundary for single-admin servers.
- Current selectable ingress modes are `none` and `cloudflare-tunnel`.
- Cloudflare tunnel lifecycle can create or adopt an account tunnel. Ingress
  route commands record pending local metadata only; routes are not made
  publicly routable yet.
- Privileged remote actions require the remote admin sudo password.
- During first server creation, users choose the remote admin sudo password and
  must save it for later serverpro operations.
- Users should change or disable default provider root credentials after first
  access, according to provider recovery and console guidance.
- Unrestricted `NOPASSWD:ALL` sudo is a failing managed-state condition.
- Tailnet DNS is part of the access path: with MagicDNS enabled, the tailnet
  must define global nameservers and enable Override DNS servers, or managed
  hosts can lose all public name resolution without any local change.

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
errors, docs, plans, tests, or issue reports. Create/bootstrap progress is
stderr-only and accepts fixed phase constants, elapsed time, and attempt count;
it never formats operator/provider values. Hermetic full-chain CI uses only
sentinel credentials and redacts them from failure artifacts before upload.
Provider account tokens are marked non-serializable so even request DTO JSON
omits them. Redacted error messages
retain wrapped causes for `errors.Is`/`errors.As` decisions while their public
message stays masked. Remote admin sudo passwords are runtime-only. Direct
user-supplied Tailscale auth keys are rejected; serverpro creates short-lived
tagged keys. Doctor remote batching base64-frames command output and reports
nonzero status without copying command text or output into batch errors.
Per-command, decoded-aggregate, and transport byte ceilings return typed errors
before hostile output can grow memory without bound. Read commands come from an
explicit plan; unplanned reads never fall through to live execution, and
per-command overflow, frame, or transport failures fail closed before any
requested remediation runs.

## Supply-chain verification

Managed host tools install through integrity-checked sources, not
`curl ... | sh`:

- Docker is added via its official apt repository only when the downloaded
  keyring contains exactly one primary key with fingerprint
  `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`; subkeys are allowed, but missing,
  substituted, or additional primary keys fail before publication.
- mise is fetched as a release tarball and verified against its published
  SHA-256 checksum before the target user installs it into `~/.local/bin`.
- uv is pinned and installed through mise's explicit `aqua:astral-sh/uv`
  backend. That registry entry advertises release SHA-256 and GitHub-attestation
  verification; doctor verifies the resulting exact uv version.
- Rust is pinned and installed through mise's `core:rust` backend with the
  default rustup profile. Doctor verifies rustc, Cargo, rustfmt, Clippy, and Rust
  docs. rustup downloads over HTTPS, but its official security documentation
  states download signatures are not yet enforced; this remains a trust
  boundary despite the exact version pin.
- ast-grep, sem, and inspect are installed from exact mise GitHub backends
  and versions. Their x86_64/arm64 release assets are pinned by reviewed
  SHA-256 values; unsupported architectures fail before mutation. Doctor checks
  exact ast-grep and sem version output. Existing active `sg` mise configuration
  is removed through mise's config-aware `unuse` operation without invoking the
  deprecated binary. Inspect has no upstream version flag, so its bare release
  binary is hashed before any inspect execution and reported with
  sanitized evidence. A failed integrity probe forces same-version replacement.
- Herdr is installed for the target user through mise's explicit GitHub backend
  at a pinned version; bootstrap and doctor verify the resulting Linux binary
  against the architecture-specific SHA-256 digest published with that GitHub
  release. Bootstrap installs the bundled Pi integration under the target user's
  private Pi agent directory and does not invoke Herdr self-update or session
  lifecycle commands.
- Tailscale `1.102.2` first-boot and repair binaries are fetched only for
  reviewed amd64 or arm64 architectures, checked against pinned SHA-256 digests,
  and extracted by exact member name. Unsupported architectures and mismatches
  stop before install. Serverpro explicitly sets `GODEBUG=tlsmlkem=1` for
  `tailscaled` to retain hybrid TLS negotiation. Live repair publishes each
  verified file atomically and schedules the daemon restart after the Tailscale
  SSH update command returns.
- Cloudflared's apt key is downloaded to a temporary file whose keyring must
  contain exactly one primary key with fingerprint
  `CC94B39C77AE7342A68B89628A682D308D4E5E73`. Missing, substituted, or additional
  primary keys fail before the key can enter the trusted keyring.
- The bootstrap script runs as root, including system-package and apt work plus
  managed-artifact download, verification, extraction, and staging. Read-only
  doctor uses existing apt candidate metadata; `--fix` refreshes metadata before
  upgrading the declared package set. Final user-home installation runs as the
  target user; root does not write into user home paths.

Treat fingerprints, versions, and checksums as reviewed trust roots. Rotate them
only after checking the new value through the official source recorded in
`WEB_SOURCES.md`, reviewing release/security notes, updating executable mismatch
coverage, and landing code plus documentation together. A changed remote key or
artifact fails closed until that review completes.

Release tags run the reusable non-live check gate on the exact tagged commit and
use the same Go 1.26.5 pin as local development. Release publication rejects
non-SemVer tags and existing releases, never clobbers assets, smoke-tests native
binaries, and pairs every target archive with checksums, an SPDX SBOM, and
signed Sigstore provenance and SBOM attestations. SemVer prerelease tags are
published as GitHub prereleases. Repository-level tag protection and
immutable-release settings remain required defense in depth.

## Safe operations

Power and delete operations require:

- local state-known provider IDs
- provider identity and namespace/server ownership metadata
- typed managed-resource references for access-policy cleanup; legacy adapter
  keys are read-only migration input, and conflicting identities fail closed
- confirmation gates
- Cloudflare tunnel deletion only when state proves `created` provenance;
  adopted, imported, unknown, and legacy provenance are retained

State is removed only after provider deletion succeeds. Registry reads and
workflow-lock creation use root-scoped filesystem operations so symlinks cannot
escape the selected registry parent or nearest accessible lock-path ancestor.
Create, import, and single-server delete acquire one state-owned
shared-namespace/exclusive-server workflow lock in canonical order and release
in reverse. Nonblocking flock retry honors command cancellation and deadlines.
Namespace create/delete take the namespace lock exclusively. Namespace delete rejects canonical config,
credential, state, or import-marker artifacts lacking registry authority before
approval, then revalidates the complete registry set plus each parsed state or
missing-state status after locking, so partial imports and replacement authority
survive.
Create parses its exact source-byte snapshot and conditionally publishes it under
a config-file lock after acquiring the server lock; source appearance, removal,
edits, and competing managed writes fail closed. Config recovery updates read
and publish under the same lock. Delete first requires registry routing to match
the approved state path, then revalidates state, registry paths and resource
names, cleanup selectors, and credentials; cleanup cannot replace the validated
state path before the first provider mutation. Create/import registry
publication and reconciliation share a per-tailnet lock; token-relative identity
takes a global policy guard because it may address any tailnet. Per-server delete
never mutates tailnet-global ACL policy. `tailnet reconcile` is the sole destructive
policy cleanup path: it requires an explicit stable tailnet identity, fails
closed on unreadable or unresolved registered evidence, and combines only
matching-tailnet registered tags with live-device tags. It removes exact
serverpro-owned tag shapes and only stale destinations from recognized mixed
SSH rules; tags referenced as owners by retained definitions remain protected.
Unmodeled rule fields survive approved destination rewrites. Preview with
`--dry-run`; live apply requires confirmation or
`--yes`, refetches, and aborts unless the fresh removal plan exactly matches the
approved preview. Every changed policy requires an ETag; a missing ETag aborts
before publication. Interactive preview is stderr-only; stdout remains one final
JSON document. Partial failures must report remaining resources. Local state can
drift from provider reality. Only an explicit not-found filesystem result is
treated as missing local state;
permission, invalid-path, and other I/O failures stop provision/import rather
than bypassing overwrite protection. Admin-username recovery creates a minimal
config only when the file is absent; malformed or unreadable config is returned
to the operator unchanged.

`serverpro server discover` lists compute resources labeled
`managed-by=serverpro` on a provider token. For managed Hetzner resources,
discovery also requires one exact ownership-labeled access-policy match.
DigitalOcean canonical policy requires exactly one namespace/server-derived
firewall target tag and no direct droplet-ID attachments. Recovery also accepts
the complete historical ownership-tag selector set only when full live Droplet
inventory proves no unrelated match; missing, ambiguous, foreign, incomplete,
or otherwise broadened selectors fail closed. Resumed creation
refetches every checkpointed access policy before compute mutation and rejects
foreign identity, broadened rules/selectors, or direct attachments; Vultr may
reconcile only missing required rules. DigitalOcean deletion likewise fetches
and validates both the tracked Droplet and firewall before either DELETE.
Historical firewalls with exact ownership-tag selectors require this bounded
inventory proof for import and deletion; missing/extra selectors, direct
attachments, inventory failure, or another match aborts before mutation. The
`serverpro server import` command rebuilds local config, credentials, typed
policy state, and registry from that inventory after the operator re-supplies
tokens. Optional `--with-tailscale` and `--with-cloudflare` reattach mesh/tunnel
metadata when those APIs are reachable. Provider-only recovery keeps mandatory
Tailscale access enabled while storing an incomplete private credential set for
later interactive completion. Forced import repairs the disabled-Tailscale
config emitted by earlier releases and merges omitted service tokens from the
existing server-scoped credential file instead of erasing them. It then
refreshes provider and explicitly enriched identities while preserving operator
intent and non-recoverable policy evidence. Conditional config publication
rejects concurrent appearance or edits;
malformed or unreadable artifacts fail closed. Matching transaction retries
retain that preserved baseline. Stronger tunnel provenance is preserved only
when enrichment rediscovers the same tunnel. Imported tunnels
remain non-owned and are never deleted by server cleanup. Import never stores
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
