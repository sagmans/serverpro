# Security

Treat every non-dry-run command as live infrastructure work.

## Supported versions and platforms

Until `v1.0.0`, only the latest release and `main` receive security fixes.
Older tags are best-effort.

The supported controller is macOS 27 arm64. Managed hosts are Ubuntu 24.04 LTS
(`noble`) on amd64 or arm64. Live create validates the selected provider catalog
image before provider mutation; repair scripts validate the actual host before
managed mutation. Cloud-init repeats the host check as defense in depth rather
than acting as the sole gate. Go 1.27.3 is the
source-build, development, CI, and release floor. `INSTALLATION.md` records the
complete reviewed tool and package baseline.

Exact artifact/runtime pins fail on drift. Direct apt dependencies use reviewed
minimums instead: after repository refresh, needed signed candidates must meet
the floor before package scripts run, installed results are verified, and newer
security updates remain valid and are never downgraded. The one exception is a
cloudflared connector that serverpro did not install: the cloudflared install
and repair replace it with the Cloudflare build, even when it is newer. This
avoids freezing security fixes while ensuring every
previously unversioned direct dependency has an enforced baseline.

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
- Create binds the server to the one tailnet device that enrolled with its
  single-use bootstrap key, judged by control-plane creation time (the local
  clock minus a skew margin when the API omits the key's time), before any
  secret or bootstrap script leaves the controller. Several matching devices,
  or a recorded device that is gone or renamed, stop create and doctor instead
  of choosing one. Doctor and reruns check the recorded device ID, not the name.
- Public SSH is disabled.
- OpenSSH is off by default (`access.openssh: disabled`). Create turns it off
  as its last remote step, after Tailscale SSH has carried the bootstrap. The
  step stops before any change when `tailscaled` is down, Tailscale SSH is
  off, an OpenSSH session is established, an OpenSSH login is in the last 30
  days, or a cloudflared ingress routes to SSH. That covers every config
  cloudflared can read on the host (a unit's `--config`, and its default
  config directories under every home and on the system), a `--url` or
  `TUNNEL_URL` origin in a cloudflared unit, its environment files, or
  `/etc/default/cloudflared`, and the tunnel's remotely managed
  ingress, which create and `doctor --fix` read from the Cloudflare API
  before they change the host, with configuration-wide origin settings
  applied to every rule and any private network or hostname route to the
  tunnel counted as a route, since WARP clients reach every port through
  one. On the host,
  every `service:`, `url:`, `--url`, and `TUNNEL_URL` origin must pass the
  same allowlist as the remote ingress, with ports compared as numbers so
  `0022` is port 22; bastion and SOCKS settings in a config, flag, or
  variable stop the step; and a grep for `ssh://` and port 22 with leading
  zeros backs the allowlist up. The config search covers every `HOME` a
  cloudflared unit sets as well as the account database's homes, and a
  cloudflared process counts by its name or by its executable. A failed API
  read, or an answer that does not name the tunnel, stops the step, so a
  route the read could not see never loses its origin. So does a host config
  that cannot be read, and a cloudflared process outside the cloudflared
  units, such as one in a container, whose settings are not on the host.
  The host scan prints no value, only the kind of setting, because those
  settings hold the tunnel token. It masks both ssh units and creates
  `/etc/ssh/sshd_not_to_be_run`, so neither a dependency nor a
  package upgrade starts sshd again. Login reports name the method and user,
  never the source address. The packages stay installed for updates and
  rollback, and the sshd hardening checks still check the config.
- Public app ingress defaults to `none`.
- Post-bootstrap egress lockdown is fail-closed: an omitted
  `phase_lockdown_after_bootstrap` value behaves as `true`; only explicit
  `false` disables it. Restricted egress still permits outbound SSH (22/tcp)
  for git-over-SSH workflows. Create, `server bootstrap`, and
  `serverpro server doctor NAME --fix` all apply the same lockdown under the
  same rule (restricted mode and the phase not `false`), so doctor never
  checks or re-applies a lockdown the operator turned off, and it
  exits non-zero unless UFW denies outgoing, a non-allowlisted connect is
  blocked, and the Tailscale coordination server is still reachable. A create
  that cannot reach the host fails instead of completing open.
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
- The required full-development GitHub PAT enters only through a masked prompt
  and travels over SSH stdin. It is written as a `0600` gh `hosts.yml` on the
  managed host and stored locally as plaintext `github_pat` under the
  stored-operator-auth model below. It never appears in local config, state,
  logs, or process arguments: remote validation hands it to curl through a
  stdin config instead of the command line.
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
- Unrestricted `NOPASSWD:ALL` sudo is a failing managed-state condition. The
  only exception is the first-boot grant in
  `/etc/sudoers.d/90-serverpro-bootstrap`, which create removes over Tailscale
  SSH right after the host is reachable and before any other remote step.
- Cloud-init user data never carries the admin password or a hash of it, since
  any process on the host can read user data from the provider metadata
  service. The admin account starts locked; serverpro sets the password after
  first boot with plain `chpasswd` over protected stdin, so the host stores it
  with its default scheme (yescrypt). Doctor fails `admin password hash` when
  the stored hash uses SHA-crypt (`$6$`, written by older releases) or another
  legacy scheme, or when the password is unset; `doctor --fix` re-sets it the
  same way. Hosts provisioned by older releases can still serve that hash from
  metadata: the `metadata user-data` doctor check fails when user-data holds
  the admin's current hash and warns on any other hash or `passwd` key. It
  reports presence only and keeps the body, hash, and shadow entry out of
  output and reports. The compute provider adapter declares the metadata path;
  doctor never takes it from the host.
- The gated reboot (`reboot.enabled`, on by default) applies kernel and
  library updates without a blind reboot. It runs as root, so it is a
  root-owned `0755` script with a fixed `PATH` and `LC_ALL=C`, bounds each
  systemctl, Docker, and `ss` call with a timeout, and fails closed: a host
  state it cannot read skips the reboot, including containers that run
  while `docker.service` is stopped. Its snapshot and result in
  `/var/lib/serverpro/` are root-only `0600` files and hold unit and
  container names, images, health, listening addresses, and the boot ID
  only, never environment variables, command lines, or secrets. After boot
  it restarts only a workload from the snapshot, at most once, and only
  when the snapshot comes from the previous boot and is at most one hour
  old: a stale snapshot never starts a workload that the operator stopped
  since. A stopped or failed check never reads as a checked boot. Doctor
  compares the installed script and units with the rendered copy by SHA-256,
  mode, and owner.
- Doctor drift checks report what weakens this model after provisioning:
  restricted egress not in force (UFW policy, every lockdown rule, and a real
  connect to a port the lockdown never allows), OpenSSH running or able to
  start while config wants it off, a pending reboot, a running workload that
  a reboot would drop, a gated reboot whose files or units differ from the
  rendered copy,
  unattended-upgrades options that differ from the serverpro drop-in (which
  turns on unused-kernel removal so `/boot` cannot block a kernel update,
  and keeps unattended-upgrades from rebooting on its own),
  extra accounts in
  `sudo`/`admin`/`lxd`/`docker`, with a sudoers rule, or with uid 0, provider
  image accounts, a Tailscale `OperatorUser`,
  Tailscale Funnel or Serve entries with no local listener, runtime sysctl
  values that differ from the serverpro drop-in, and a cloudflared service that
  is enabled or running on a host whose config ingress is `none`. Restricted
  egress has a repair (the provisioning lockdown), because config states the
  wanted policy, and so do OpenSSH off (the create step, safety checks
  included), the unattended-upgrades drop-in, and the gated reboot's own
  script and units. Doctor never reboots and never changes a workload's
  restart policy or unit: only the operator knows whether a workload must
  come back. Privileged accounts has a repair only for the accounts the
  compute adapter declares as its image accounts; every other account,
  sudoers rule, or group member is report-only, because it may run a
  workload such as a CI deploy. Doctor reads the rules with sudo's own
  parser (`cvtsudoers`: JSON, aliases expanded, every include followed),
  never with line patterns that a continued line, a user list, a `#uid`, or
  `!!` can get past, and the check fails closed when the parser is missing,
  fails, or prints output doctor cannot read. The parser reads only
  `/etc/sudoers` and its includes, which is all sudo reads under Ubuntu's
  stock `/etc/sudo.conf` and `/etc/nsswitch.conf`. Serverpro assumes that
  layout and checks it: doctor fails, and the image account removal stops
  before any change, when `sudo.conf` loads a plugin other than
  `sudoers.so`, passes `sudoers.so` an option other than `error_recovery`
  (`sudoers_file`, `sudoers_uid`, `sudoers_gid`, `sudoers_mode`,
  `ignore_perms`, or one it does not know), or sets a `Path` entry other
  than `askpass` (which runs as the caller) to anything but its packaged
  value: `plugin_dir`, `noexec`, `intercept`, and `sesh` name what sudo
  loads into or runs as root, and any other name fails. It does the same
  when the first `sudoers:` line in `nsswitch.conf` names a source other
  than `files` (SSSD or LDAP rules that neither check can read). A sudoers
  rule without a command limit fails, and so does a rule that lists a
  root-equivalent command (a shell, editor, pager, interpreter, file writer,
  loader, or account, package, service, module, capability, or container
  manager) by any path or arguments, a rule with `SETENV` or an option
  doctor does not grade as safe, and a `Defaults` entry, whatever it binds
  to, that doctor's allowlist does not pass. The allowlist names each option
  doctor grades as safe with the values it may hold, so an option it does
  not list (`env_file`, `mailerpath`, `runchroot`) fails, and so does a
  value it cannot grade. `env_keep` and `env_check` may add only the names
  sudo itself keeps or checks after `env_reset`, less `PATH`; a wildcard
  passes only inside one of them (`LC_*`). Each `secure_path` directory and
  the `logfile` and `iolog_dir` paths must pass the same root-only path walk
  as a listed command, and a relative entry, a `%` escape, or no
  `secure_path` at all fails. A rule that lists only other absolute command
  paths warns and names them. On the server, every hop of each listed path,
  each symlink and the directory it sits in included, must be root-owned and
  not writable by group or others, and the last hop must be a regular file
  that is not a root-equivalent command; otherwise the rule fails, because a
  symlink or a writable script runs anything as root behind a narrow-looking
  path. A listed path also fails when a file with the same name plus a space
  exists, since the parser's output cannot tell an escaped space from
  arguments. Give such an account a root-owned wrapper script that it cannot
  write and that runs one fixed action. See USAGE.md, Drift checks and
  Provider image accounts.
- Tailnet packet rules are outside serverpro's ownership: serverpro edits only
  its tag owners and SSH rules. Doctor warns when the tailnet policy lets every
  source reach every port, since UFW is then the only barrier between any
  tailnet device and every listening port. See `docs/tailscale-setup.md`,
  Recommended grants policy.
- Human SSH accept (`access.tailscale.human_ssh: accept`, off by default)
  trades step-up login for direct access. The node key of every tailnet admin
  device becomes the credential for the admin user. Every process on those
  devices gets a shell, and with it sudo through a stored password, with no
  browser login. Controls: config and the policy write refuse `root` as the
  admin user with this value, before any write; the rule names only the admin
  user, so root stays `check` or disabled; the read-back fails closed when the
  rule is not `accept` or a root rule is not `check`; and doctor fails on an
  `accept` human rule that config does not ask for.
- Automation SSH (`access.tailscale.automation`, off by default) trades
  step-up login for unattended runs. Every process that can open the
  node's LocalAPI socket gets admin SSH, and with it sudo through the
  stored password, on every server whose rule names the tag, with no
  browser login. Controls: the tag is fixed and config cannot name a
  server tag as the source; the `accept` rule names only the admin user,
  so root stays `check` or disabled; config and the policy write refuse
  `root` as the admin user while automation is on, before any write; the
  human rule stays what `human_ssh` asks for; and
  the read-back fails closed when a root rule is not `check`. The
  transport uses `-F /dev/null`, `BatchMode`, and strict host keys from
  the node's own peer status, the system `/usr/bin/ssh`, and no key or
  agent offered. The target must be exactly one peer that carries a
  server tag. Keep the socket in a 0700 directory, run the node in
  userspace mode with no SOCKS5 or HTTP proxy, and grant the tag `tcp:22`
  only. The `accept` rule is shared by servers with the same SSH rule
  tags and admin user, so turning automation off for one server does not
  revoke it while another registered server records it. A rule reaches
  every node with any of its `dst` tags, and all servers in a namespace
  share the namespace tag, so separate servers in one namespace need
  their own tag in `ssh_tags` (USAGE.md, Separate servers in one
  namespace); config accepts only tags that the server itself carries.
  Doctor fails on a missing `accept` rule and warns on a left-over or
  foreign one, also one that reaches the server through a shared tag. To
  revoke for all, set `enabled: false` on every server that shares the
  rule, run `server ssh-policy` for each, then log the node out and
  remove its device.
- Tailnet DNS is part of the access path: with MagicDNS enabled, the tailnet
  must define global nameservers and enable Override DNS servers, or managed
  hosts can lose all public name resolution without any local change.

## Credentials and secrets

Provider and service credentials plus stored operator auth live in server-scoped
files:

```text
~/.config/serverpro/namespaces/<namespace>/servers/<server>/credentials.json
```

Credential directories must be `0700`; files must be `0600`.

Stored operator auth, plaintext at rest by operator decision: the remote admin
sudo password (`admin_sudo_password`, gated by `admin.store_console_password`,
default on, `false` keeps it runtime-only), the GitHub fine-grained PAT
(`github_pat`, redeployable to a GitHub-rejected remote `gh` credential by
`serverpro server doctor --fix`), and an optional user-supplied Tailscale auth key
(`tailscale_auth_key`, refused at provision time). Sudo-password resolution
order is: in-process cache,
`<NAMESPACE>_<SERVER>_SUDOPASS` env var, stored file, prompt; prompted or
env-supplied values are persisted when the flag is on. Unstamped legacy
configs migrate their tool-forced `false` to stored on load; `schema_version: 1`
files keep an explicit `false` as runtime-only. Disk compromise or a
leaked backup/home copy exposes these secrets — protect the home directory and
backups accordingly.

`serverpro server doctor` compares the stored `github_pat` with the remote
`gh` token through truncated SHA-256 fingerprints, so drift is diagnosable
without either credential ever appearing in evidence or logs. Doctor asks
GitHub directly about the remote token instead of trusting the remote `gh`
toolchain, so a missing or unrepaired tool never reads as a dead credential:
only a token GitHub rejects (or a host with no token at all) is replaced, a
working but different token is reported for a human decision, and an
unanswered probe stays unresolved with the egress lead.

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
message stays masked. Sudo passwords may now persist in credentials.json per
the stored-operator-auth model above; every other handling rule is unchanged.
serverpro always creates short-lived tagged Tailscale keys itself, and a
stored user-supplied auth key is refused rather than used because its
namespace scope cannot be verified; the value is never logged or exported.
Doctor remote batching base64-frames command output and reports
nonzero status without copying command text or output into batch errors.
Per-command, decoded-aggregate, and transport byte ceilings return typed errors
before hostile output can grow memory without bound. Read commands come from an
explicit plan; unplanned reads never fall through to live execution, and
per-command overflow, frame, or transport failures fail closed before any
requested remediation runs. The first remote doctor check is a blocking
OS/codename/architecture authority check; failure disables every later fix in
both batched and sequential execution. Package/tool diagnostic results retain
captured output, but generic transport errors remain free of batch output.
The CLI redacts known configured and runtime credentials before failure
evidence truncation. JSON reports limit each failure excerpt to 4 KiB and
preserve UTF-8 boundaries. Raw failure output is not written to a separate log.
A failed provider API call names its endpoint with Cloudflare-style opaque
IDs (32-hex account and zone IDs, UUID tunnel IDs) shown as `{id}` and no
query string, in both the status and the transport error, and repeats at
most the first 256 bytes of the response, with IDs masked, so evidence does
not carry the account or tunnel a call targeted or the provider's whole
answer. Delete's tunnel ownership check masks both tunnel IDs the same way
when the live tunnel is not the one in state.

Server doctor and create save the complete redacted report in the system
temporary directory, including when `--full` is selected. Check evidence
retains the same size limits as stdout. Reports use
`0600` files beneath private `0700` directories. Predictable per-user names
are not a security boundary: persistence rejects untrusted ownership, unsafe
directory permissions, symlinked managed paths, and target path traversal.
Retention attempts to keep the newest 5 reports per server and leaves
unrelated files intact. Operating-system cleanup can remove these files.
Reports still contain infrastructure metadata; treat them as private when
copying or sharing them. A storage failure produces a redacted stderr warning
and preserves the complete report on stdout.

A stored server token goes only to the provider recorded in that server's state.
The `--provider` flag cannot redirect it. A command with a different flag value,
or state with no provider to compare, fails closed before it uses the token. A
`server create` rerun also checks an existing state before it changes the config
or runs preflight. The rerun records the flag value only when state has no
provider and no provider resources. Each compute adapter also refuses a record
of another provider before its first read or mutation call.

Provider API calls never follow redirects that leave the credential's trust
boundary. The shared provider HTTP client refuses redirect chains after five
hops, rejects plaintext and cross-host targets outright (subdomains and other
ports count as cross-host), and refuses 307/308 responses on endpoints that
carried a request body, so bearer tokens and bootstrap payloads containing
Tailscale auth keys or password hashes cannot be laundered to a second origin.
A caller-supplied HTTP client keeps its own redirect policy; only clients
without one get this secure default.

## Supply-chain verification

The 2026-10-03 managed-tool review, refreshed 2026-10-22 and 2026-10-30, selects
stable releases at least seven days
old, except explicitly approved fixes for disclosed vulnerabilities. Tailscale
`1.104.4`, Docker Engine `29.9.2`, Buildx `0.38.1`, Compose `5.6.1`, mise `2026.10.24`,
Node.js `24.22.1`, uv `0.13.5`, gh `2.103.3`, and sem `0.29.0` use those
exceptions; `WEB_SOURCES.md` records the evidence. The age
threshold governs release selection. Signed apt candidates can be newer.
The serverpro-owned Pi lockfile is generated under seven days through npm’s
`--min-release-age` option, with each exemption recorded in `WEB_SOURCES.md`.
containerd `2.4.2` meets seven days and
fixes `CVE-2026-95839`, `GHSA-8647-2jxj-p2rc`, and `GHSA-hc94-p972-ch66`.
Docker Engine, Buildx, and Compose are rebuilt with Go releases that reject
the eight revoked eMudhra intermediate certificates (`CVE-2026-111943`).
The mise exception fixes `GHSA-wcqh-j26q-g44x`: untrusted inline
`.tool-versions` options can disclose `GITHUB_TOKEN` on releases through `2026.9.18`.
A 2026-10-22 follow-up exception raises the mise floor from `2026.10.0` to
`2026.10.17`. That release stops mise’s built-in npm backend from trusting
eight eMudhra intermediate CA certificates whose private keys were compromised.
The same 2026-10-22 review takes Node.js `24.22.1` for its security release,
uv `0.13.5` for `GHSA-hq85-5cpj-mr62` (its own trust store kept the compromised
emSign root), and gh `2.102.1` for its Go `1.27.3` standard-library fixes,
all younger than seven days. ast-grep `0.51.0` passes the age gate and fixes
critical `GHSA-xx6h-xcfw-vf5j` and high `GHSA-cmvf-x8f7-rvp6`: a project
configuration could load native code as a custom language library.
The 2026-10-30 Tailscale exception fixes Tailscale SSH forwarding to root-only
unix sockets (TS-2026-014), an SSH memory denial of service (TS-2026-015), and
Serve flaws (TS-2026-016, TS-2026-017); it keeps the TS-2026-013 eMudhra fix.
The same review raises the mise floor to `2026.10.24`: pinned checksums, Node
signatures, and attestations fail closed, the GitHub token goes only to GitHub
API hosts, and git refs can no longer inject options. gh `2.103.3` carries the
`digitorus/pkcs7` fix for `GHSA-3xqc-rpq8-wgp9`, and sem `0.29.0` drops the
compromised eMudhra CAs from its bundled roots.
`MISE_SAFE=1` skips mise’s trust check; never use it to inspect untrusted projects.

Buildx `0.38.1` is in Docker’s Noble apt repository for both supported
architectures at review time. The fallback stays for hosts whose apt candidate
is older. Bootstrap instead permits only the reviewed
official binary with a Go-owned architecture-specific SHA-256. It verifies
the digest before execution and before Docker package scripts. Vulnerable
`0.37.1` is never installed as the fallback. All other apt preflights remain.

Managed binaries use a protected root-owned namespace and atomic publication.
Doctor checks Docker’s plugin search order before execution. It rejects
configuration-directory entries with line breaks rather than reinterpret their names.
Failed package convergence leaves the verified fallback alias intact. It rejects
unrecognized overrides, writable managed ancestors, wrong ownership, altered
artifacts, and loaded versions below the floor. Repair updates the managed
artifact; safe apt availability triggers migration without deleting operator
plugins. Docker warns that manual downloads do not receive automatic security
updates. Keep ServerPro pins reviewed and run doctor with repair. Checksums
prove agreement with reviewed bytes, not absence of upstream compromise.
Release age and checksums do not prove that an artifact or its dependencies
are uncompromised. Pi 1.x publishes no `npm-shrinkwrap.json`, so a plain
`npm install -g` would resolve each transitive package to the newest match at
install time. Serverpro owns the Pi lockfile instead. The binary embeds it and
refuses to start unless it installs exactly the pinned Pi version, and unless
every package is a production download from `registry.npmjs.org` with SHA-512
integrity. Bootstrap verifies the lockfile digest, then installs it with
`npm ci`, which rejects any integrity mismatch. Bootstrap passes the registry
to npm, so a host or user `.npmrc` cannot redirect the downloads. Lifecycle scripts
are disabled and development dependencies are omitted. Bootstrap and doctor
check the
lockfile digest and every installed package version from metadata only,
without executing dependency code. Bootstrap also requires a clean runtime
dependency audit before the new tree replaces the old one. Audit failure or
unavailability stops the installer; an exact Pi CLI version alone is
insufficient. `make check` audits the lockfile, so a new advisory blocks the
next push.

Managed host tools install through integrity-checked sources, not
`curl ... | sh`:

- Docker is added via its official apt repository only when the downloaded
  keyring contains exactly one primary key with fingerprint
  `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`; subkeys are allowed, but missing,
  substituted, or additional primary keys fail before publication.
- mise `2026.10.24` is fetched as a release tarball and verified against its
  published SHA-256 checksum before the target user installs it into
  `~/.local/bin`; newer compatible mise releases remain valid.
- uv `0.13.5` is pinned and installed through mise's explicit
  `aqua:astral-sh/uv` backend. That registry entry advertises release SHA-256
  and GitHub-attestation verification; doctor verifies the exact version.
- Rust `1.99.1` is pinned and installed through mise's `core:rust` backend with
  the default rustup profile. Doctor verifies rustc, Cargo, rustfmt, Clippy, and
  Rust
  docs. rustup downloads over HTTPS, but its official security documentation
  states download signatures are not yet enforced; this remains a trust
  boundary despite the exact version pin.
- ast-grep `0.51.0`, sem `0.29.0`, and inspect `0.1.1` are installed from exact
  mise GitHub backends and versions. Their x86_64/arm64 release assets are
  pinned by reviewed
  SHA-256 values; unsupported architectures fail before mutation. Doctor checks
  exact ast-grep and sem version output. Existing active `sg` mise configuration
  is removed through mise's config-aware `unuse` operation without invoking the
  deprecated binary. Inspect has no upstream version flag, so its bare release
  binary is hashed before any inspect execution and reported with
  sanitized evidence. A failed integrity probe forces same-version replacement.
- Herdr `0.9.5` is installed for the target user through mise's explicit GitHub
  backend; bootstrap and doctor verify the resulting Linux binary
  against the architecture-specific SHA-256 digest published with that GitHub
  release. Bootstrap installs the bundled Pi integration under the target user's
  private Pi agent directory and does not invoke Herdr self-update or session
  lifecycle commands. Doctor verifies the digest before it runs Herdr at all.
  On a mismatch it reads the installed version from the mise install path
  without running the binary, so an older pinned install reports as
  `expected herdr X, got herdr Y`. Any other mismatch reports as a SHA-256
  mismatch.
- Tailscale `1.104.4` first-boot and repair binaries are fetched only for
  reviewed amd64 or arm64 architectures, checked against pinned SHA-256 digests,
  and extracted by exact member name. Unsupported architectures and mismatches
  stop before install. Serverpro explicitly sets `GODEBUG=tlsmlkem=1` for
  `tailscaled` to retain hybrid TLS negotiation. Live repair publishes each
  verified file atomically and schedules the daemon restart after the Tailscale
  SSH update command returns.
- Cloudflared's apt key is downloaded to a temporary file whose keyring must
  contain exactly one primary key with fingerprint
  `CC94B39C77AE7342A68B89628A682D308D4E5E73`. Missing, substituted, or additional
  primary keys fail before the key can enter the trusted keyring. The installer
  uses the explicit noble repository and requires cloudflared `2026.10.6` or
  newer; ingress-enabled doctor runs verify both that floor and service state.
  Older connectors still trust eight eMudhra intermediate CAs whose private
  keys were compromised, and builds before `2026.10.6` also ship vulnerable
  go-jose and go-chi modules and expose the metrics debug endpoints, so
  doctor fails them and `--fix` upgrades them.
  With ingress `none`, doctor still audits a connector that another tool
  installed, because the config setting does not prove the host has none. An
  enabled or running connector gets the full check and repair. A dormant
  package gets the floor and receipt check, and `--fix` reinstalls it from the
  trusted origin without the token and without starting it, so the repair
  never opens ingress. Any host state the audit cannot classify, such as a
  half-installed package, a `cloudflared` elsewhere on `PATH`, or a connector
  process outside the service, fails closed with no change.
  Install and repair pin the `cloudflared` package to origin
  `pkg.cloudflare.com` (apt preferences, priority 990) so apt prefers it. The
  pin only ranks sources. The control is the origin gate: install and repair
  install only the newest version that the Cloudflare origin alone offers, so
  a version offered only or also by another source is refused, even at a
  higher version.
- After each install, serverpro writes the installed version and the SHA-256
  of `/usr/bin/cloudflared` to `/var/lib/serverpro/cloudflared.receipt`
  (root, `0644`). Cloudflare's signed index lists only the latest release, so
  apt cannot vouch for an older installed version; the receipt can. The
  doctor check fails when the receipt is missing or when the installed
  version or binary digest differs from it. This catches a connector from
  `dpkg -i` or another repository, a same-version replacement, and an edited
  binary, even above the floor. The repair then reinstalls the Cloudflare
  build with `--reinstall`, and adds `--allow-downgrades` only to replace a
  newer foreign build. The receipt proves only what serverpro installed; root
  on the host can rewrite both the binary and the receipt.
- The doctor sshd repair validates a candidate config with `sshd -t -f`
  before it touches the live files, writes the drop-in before the `Include`
  that activates it, and restores the previous files if the live config then
  fails `sshd -t`. It validates the restored files again, and a restore that
  fails or leaves an invalid config exits with a distinct status that names
  the files, so the operator fixes them while a session is still open. It
  reports success only when `sshd -T` shows every hardened value.
- Every directly installed Ubuntu, Docker, and Cloudflare apt package has a
  reviewed minimum in `internal/hostplatform`. Installs preflight signed current
  candidates before package scripts run, reject missing/below-floor packages,
  force the C locale while parsing apt output, require dpkg state `installed`
  rather than trusting stale `config-files` version metadata, verify installed
  floors, preserve newer versions, and never request a downgrade, except to
  replace a cloudflared connector that serverpro did not install.
- apt work waits for a concurrent apt run within one bounded budget
  (`internal/hostplatform`, 300 seconds) rather than failing or hanging.
  Package-list refreshes retry only apt's C-locale package-lists lock error;
  repository, signature, and network errors are never retried or hidden, and
  keep apt's exit status and output.
- Node.js `24.22.1` LTS and bundled npm `11.19.1` are exact. Node/npm drift
  forces same-version Node replacement; `pi` and `all` reinstall Pi afterward
  because replacing Node removes the Pi tree under it. Pi `1.5.0` is
  installed from the serverpro-owned lockfile with lifecycle scripts
  disabled. Packages still download from the npm registry, so enabling Pi
  keeps a registry trust boundary that the pinned integrity hashes limit.
- The bootstrap script runs as root, including system-package and apt work plus
  managed-artifact download, verification, extraction, and staging. Read-only
  doctor uses existing apt candidate metadata; `--fix` refreshes metadata before
  upgrading the declared package set. Final user-home installation runs as the
  target user through `runuser`; root does not write into user home paths. The
  drop does not use sudo, because sudo checks root's own account and fails on
  hosts that ship root with an expired password. For the same reason, remote
  doctor checks, which already run as root, never call sudo again.

Treat fingerprints, versions, and checksums as reviewed trust roots. Rotate them
only after checking the new value through the official source recorded in
`WEB_SOURCES.md`, reviewing release/security notes, updating executable mismatch
coverage, and landing code plus documentation together. A changed remote key or
artifact fails closed until that review completes.

Release tags run the reusable non-live check gate on the exact tagged commit and
use the same Go 1.27.3 pin as local development. The build that gets attested
and published does not read the Actions cache. Release publication rejects
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

After locked authority revalidation, server deletion reads and checks each
tracked external resource before compute mutation. External DELETE operations
repeat the ownership check. This preflight prevents avoidable partial deletion
when a credential or ownership check already fails. It cannot make independent
provider APIs atomic. If external cleanup fails after compute deletion,
serverpro retains local state and registry authority for a safe retry.

Canonical config, credentials, state, import markers, and adjacent server locks
are removed only after provider deletion succeeds. Custom config and state
paths remain operator-owned. Config, credential, state, and server-operation
writers share
a root-scoped local-artifact guard. Existing-server publishing workflows also
hold the namespace lock shared; delete holds it exclusively and takes the
artifact guard exclusively before cleanup so a stale managed writer cannot
recreate a removed artifact. Registry authority is removed last.
Registry reads, workflow-lock creation, and canonical artifact removal use
root-scoped filesystem operations so symlinks cannot escape the selected parent
or nearest accessible lock-path ancestor. Create and import acquire one state-owned
shared-namespace/exclusive-server workflow lock in canonical order and release
in reverse. Single-server delete holds the namespace lock exclusively across
remote cleanup and canonical local purge. Nonblocking flock retry honors command
cancellation and deadlines. Namespace create/delete take the namespace lock
exclusively. Namespace delete rejects canonical config,
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
before publication. Tailscale answers `/acl/validate` with HTTP 200 even for
failed policy tests and rejected syntax, so serverpro writes a policy only when
the validate body is the empty object `{}`. Any other body, including an empty
or unreadable one, aborts the write and reports the failing tests.
Interactive preview is stderr-only; stdout remains one final JSON document.
Partial failures must report remaining resources. Local state can drift from
provider reality. Only an explicit not-found filesystem result is
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
metadata when those APIs are reachable. Import persists the Cloudflare and
Tailscale tokens it receives, so it accepts only the serverpro-scoped
`SERVERPRO_CLOUDFLARE_TOKEN` and `SERVERPRO_TAILSCALE_TOKEN` or an interactive
prompt; a shell-wide `CLOUDFLARE_API_TOKEN` or `TAILSCALE_API_TOKEN` set for
other tools is never read or saved. Provider-only recovery keeps mandatory
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

These consoles log in on the server's console, not through sshd, so they work
while OpenSSH is off. They need the admin user's password. The DigitalOcean
Droplet Console logs in through sshd and does not work while OpenSSH is off.

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
