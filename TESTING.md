# Testing Matrix

Date: 2026-09-01
Status: living capability coverage map

serverpro changes quickly and is mostly AI-developed. Treat this file as the
source of truth for what must be tested before behavior is trusted. Capability
coverage matters as much as line coverage: every operator-visible capability
needs unit, integration, smoke/e2e, and dogfood evidence or an explicit reason
why a layer is not applicable.

## Test layers

| Layer | Command | Purpose | Live APIs |
| --- | --- | --- | --- |
| Unit | `make test-unit` | Standalone fast run of every Go package test. | No |
| Consolidated Go gate | `make test-go-check` | Run every Go test once with the race detector and one aggregate coverage profile; enforce 81.8% minimum coverage and reject 0%-covered functions. | No |
| Smoke | `make test-smoke` | Prove the binary starts, help renders, and the token-free `provider list` leaf is registered. Output is discarded. Other gates cover JSON shape. | No |
| Integration | `make test-integration` | Standalone focused rerun of CLI orchestration, provider HTTP adapters, lifecycle, import, doctor, state, and credentials with fakes or `httptest`; omitted from `make check` because the consolidated Go gate already includes it. | No |
| E2E | `make test-e2e` | Run the compiled CLI through isolated-home workflows, strict JSON parsing, inventory-derived help and parent coverage, disposition evidence, dry-runs, fixture state, and no-token safety checks. | No |
| Full-chain E2E | `make test-full-chain-e2e` | Build the test-only composition binary and run concurrent create→status→doctor→delete journeys against stateful local Hetzner, Vultr, and DigitalOcean APIs. Production doctor and cleanup orchestration consume injected local clients; fixed time, checkpoint recovery, strict JSON, cleanup evidence, and sanitized failure artifacts remain hermetic. | No |
| Release contract | `make test-release` | Run the focused release-contract Go package once plus shell assertions for the workflow DAG, exact step order/matrices, target-paired evidence, prerelease classification, toolchain pins, native smoke, and no-clobber invariants. `make check` gets the Go coverage from its consolidated suite and runs only the shell half separately. | No |
| Read-only dogfood | `make test-dogfood-readonly` | Dogfood the actual binary across every no-token command path and local state mutation path. | No |
| Live dogfood | `make test-dogfood-live` | Use real provider APIs for catalog, provider doctor, discover, and optional selectable create, status, doctor, fix, bootstrap, power, import, identity, and delete scenarios on a throwaway or kept test server; every successful command must emit valid JSON with command-specific status, shape, and identity. | Yes |
| Live harness self-test | `make test-dogfood-live-selftest` | Unit-test every importable output contract, then prove malformed/invalid-success rejection, fallback-delete evidence, guards, secret transport, and cleanup retention through the shell orchestrator with a fake binary; no tokens or network. | No |
| Shell lint | `make lint-shell` | Run `shellcheck -x` on every Git-tracked `*.sh` file, including the root bootstrap script and the Tailscale updater. Suppressions are targeted `# shellcheck disable=SCxxxx # WHY: ...` lines. ShellCheck is a system tool, not installed by the Makefile; the target fails with guidance when it is missing. | No |

The `pre-push` hook runs `make check` and then `make test-full-chain-e2e` on
every pushed commit;
the release workflow runs both again on tags (see `DEVELOPMENT.md`).
`make check` runs the primary non-live gates once; release workflow Go tests
stay in that consolidated invocation while the shell-only release contract runs
separately. The build-tagged full-chain journeys stay separate so Go
correctness, race, and aggregate coverage are not repeated inside the primary
job. Run both commands for complete non-live local parity. Live dogfood remains
opt-in so hooks, CI and local contributors do not create
paid infrastructure by accident.

## Managed-tool upgrade verification

The 2026-10-01 review updated manifest, digest, platform-floor, shell-version,
cloud-init, doctor, and real CLI help contracts. A disposable Ubuntu 24.04 arm64
container ran production mise/tool install and readiness helpers with
container-local `runuser` transport instead of sudo. Every managed user-tool
version and the Herdr Pi integration passed. No host home or credentials were
mounted; live Tailscale SSH and systemd services were not verified.

A separate disposable container verified Docker’s pinned signing key and
refreshed apt metadata. Candidate preflight rejected Buildx `0.37.1` against
the patched `0.37.2` floor before installation. The initial Pi audit found
shrinkwrapped `brace-expansion@5.0.9`. A later disposable Ubuntu run exercised
the production local repair, Pi help, idempotence, and rejection of dependency drift.
Repair restored `5.0.12` and the reviewed integrity; the runtime audit found zero vulnerabilities.
Regression tests reject old versions, changed lock metadata, wrong integrity,
and a missing shrinkwrap without executing dependency code. Since 2026-10-30
they cover brace-expansion and undici separately, reject unsafe repair manifest
rows, and require the doctor command to reach the bootstrap probe’s verdict.
The 2026-10-03 refresh repeated the complete managed user-tool installation
with mise `2026.10.0` and uv `0.12.19` in disposable Ubuntu arm64.
Canonical readiness and idempotence passed; Pi’s runtime audit remained clean.
Real uv created a working Python virtual environment. A controlled untrusted
`.tool-versions` fixture triggered mise’s trust gate; its local HTTP sink
received zero requests. The probe used only a synthetic token and no
`MISE_SAFE` override. No live systems or credentials were used.
See `WEB_SOURCES.md` for the public evidence.

The 2026-10-22 refresh ran the real bootstrap wrapper with target `all` as root
in a disposable, privileged, systemd-booted Ubuntu 24.04 arm64 container, twice.
Both runs completed, and the second run proved idempotence. Apt installed Docker
Engine `29.9.2`, containerd `2.4.2`, Buildx `0.38.1`, and Compose `5.6.1` from
Docker’s signed repository, so the Buildx fallback was not used. The Docker
daemon became active. A test container could not start because nested overlay
mounts are not supported in this environment. As the admin user, every managed
tool reported its pinned version. Real uv created a Python virtual environment.
Cargo check, Clippy, and rustfmt passed on a new crate. Linking needs a system C
toolchain, which bootstrap does not install. ast-grep matched a Python pattern.
Pi help and the Herdr Pi integration passed. Pi’s runtime audit found zero
vulnerabilities. amd64 was not run; its new asset digests were verified by download.
No host home, credentials, or live servers were used.

The Buildx fallback ran through the production Docker install and shared
diagnostic probe in disposable Ubuntu 24.04 amd64 and arm64 containers.
Real Docker CLI and Buildx version checks, repeated convergence, altered-byte
rejection and repair, and pre-execution rejection of a shadow plugin passed.
A corrupt synthetic download stopped before execution or Docker package
scripts. The full production package-doctor command passed with the fallback.
A local package fixture containing the reviewed binary verified safe-package
migration and return to package-based diagnosis on both architectures. This
fixture was not a published vendor package. Controlled directory-boundary
and failed-convergence scenarios also passed on both architectures. Ambiguous
configured directories stopped before plugin execution; failed convergence
preserved the exact verified alias and did not continue to package upgrade.

No host home, credentials, Docker socket, or privileged mode was used.
Systemd activation and firewall effects were not verified in these containers.
A container-local Docker daemon started, and the verified Buildx connected.
Image construction failed at a bind mount (`err: operation not permitted`)
under default container capabilities. An approved isolated rootless worker
later verified an amd64 OCI export, architecture, content digests, and copied
file contents. Dependency scanning stopped that worker before the arm64 build.
No further builds may use the rejected worker. These historical results do not
define the current image-build acceptance scope below.

### Local Buildx acceptance

Keep provisioning and image-build verification separate. Disposable Ubuntu
24.04 amd64 and arm64 containers verify fallback installation, checksum and
plugin selection, package preflight, doctor, and lifecycle safety.

A normal `docker build` on macOS, using the existing local Docker daemon, is
sufficient for the real image-build smoke check. Use a small `FROM scratch`
and `COPY` fixture; inspect the built image and verify its copied file contents.
Record the command, Docker and Buildx versions, image architecture, and result.
Do not claim this smoke check passed until its actual result is recorded.

The 2026-10-05 macOS arm64 smoke passed with Docker CLI `29.4.0`, local Docker
Engine `29.2.0`, Buildx `0.31.1-desktop.1`, and embedded BuildKit `0.27.0`.
The scratch/COPY build produced a Linux arm64 image. Image identity, the saved
OCI manifest/config digest chain, root filesystem layer digests, and the exact
62-byte copied fixture all matched. The sentinel SHA-256 was
`8bbd833947a6f2e9ade246e61ef6a3eaa715a04abda92efa2bba63075bd1fba8`.
The first archive verifier rejected a valid build because it treated the
containerd image ID as a legacy config digest. The corrected verifier checked
the actual OCI identity chain; the final build and all eleven Docker commands
passed. Owned images and temporary files were removed; shared build cache was
not pruned. No runtime upgrade, image execution, networked build step, external
frontend, image pull, or publication was needed. These existing local runtime
versions do not establish the managed Linux Buildx security baseline.

The macOS smoke check does not prove Linux Buildx-to-daemon integration or
native amd64 and arm64 builder execution. Those checks are outside this
accepted delivery scope; Ubuntu container checks provide provisioning evidence.
No nested Docker daemon, standalone BuildKit worker, manually managed VM, CI
runner, separate daemon endpoint, or TLS setup is required for this acceptance.
Do not mount host sockets, home directories, or credentials into test containers.
Use only disposable test artifacts; do not access live servers or publish images.

Validation stays local. Existing local gates remain in force; this documentation
change does not add or configure commit or push hooks.

Release fixtures need isolated HOME/XDG_CONFIG_HOME when operator-wide Git
hooks enforce commit-message rules on synthetic fixture commits. Keep the
real repository’s commit hooks and signing configuration unchanged.

The 2026-10-11 root-to-user drop fix kept the production transport. A
disposable Ubuntu 24.04 amd64 container ran the real bootstrap wrapper as root
with root's password expired, the state DigitalOcean images ship. Before the
fix, every drop to the admin user failed with `sudo: Account or password is
expired`. After it, target `mise` installed and verified for the admin user.
Earlier container runs above used `runuser` in place of sudo, so they could
not show this failure.

The 2026-10-11 sshd doctor fix ran on a disposable Ubuntu 24.04 arm64
container with real systemd, socket-activated ssh, and an expired root
password. Commands went through the runner's admin `sudo -S -v` and
`sudo -n sh` path. Results:

- The old nested `sudo sshd -T` check reproduced the field error. The new
  check passed.
- Nine old fixes, one per failing sshd check, failed from the sixth run with
  `start-limit-hit`, and `ssh.socket` failed too.
- Nine new fixes on hardened config passed and did not rewrite the drop-in.
- Drift with an inactive service was repaired, and the service stayed
  inactive.
- Drift with a running service was reloaded with the same main PID.
- An invalid config blocked the reload, and the service stayed active.
- The `sshd units` check named both units after the old fix left them in
  `start-limit-hit`. Its fix restored the socket listener on port 22 without
  starting `ssh.service`, and repeated runs passed.
- With the socket disabled, the fix started the enabled `ssh.service`. With
  both units disabled, it started nothing.
- Nine fixes against a running daemon with unchanged config each reloaded
  it. The main PID stayed the same and the unit result stayed `success`.
- A `mktemp` failure exited nonzero and left the drop-in unchanged.
- An enabled socket that was stopped failed the `sshd units` check, and the fix
  restarted it. An idle `ssh.service` behind an active socket passed.

## Doctor output coverage

Doctor and create tests verify non-pass summary results, status counts,
`--full` compatibility, and complete report persistence. Filesystem cases
cover private permissions, unsafe paths, retention, and storage-failure
fallback without losing diagnostics. Redaction and the 4 KiB failure evidence
limit apply to both the saved report and stdout.

Compiled full-chain journeys exercise the real CLI command paths with local
provider fixtures and isolated temporary directories. They inspect saved
reports as well as stdout; no production server or credential is required.
The Python validator and live harness self-test accept valid summaries,
including empty result lists for successful checks, but reject contradictory
status/count data and failures disguised as success.

### sshd repair validation dogfood

On 2026-10-11 the rendered sshd fix and read scripts ran in a disposable
Ubuntu 24.04 chroot with the real `openssh-server` (OpenSSH 9.6p1) and a fake
`systemctl`; the chroot was deleted afterwards.

- On stock noble config the fix wrote the drop-in, `sshd -T` reported the
  hardened values, and the read check passed. A rerun left the file untouched.
- With a broken `50-broken.conf` drop-in, real `sshd -t -f` rejected the
  candidate (from the private copy of the drop-in directory), and the live
  main config and drop-in stayed unchanged.
- With the `Include` line removed, the fix restored it at the top and kept
  both files `0644 root`. No temp files remained.

### sshd repair fail-safe dogfood

On 2026-10-11 the rendered sshd fix, sshd read, and ssh unit scripts ran in a
disposable privileged Ubuntu 24.04 container booted with systemd, with
socket-activated `ssh.socket` and OpenSSH 9.6p1. Each repair ran over a real
SSH login through `sudo -n sh`, as doctor's transport does. After every case a
fresh key login was tried and succeeded, except where noted. The container
and keys were deleted afterwards.

- Stock config: the fix exited 0. `sshd -T` showed every hardened value. The
  server stopped offering password auth, a root key login was refused, and a
  rerun rewrote nothing.
- Socket activation: `ssh.socket` stayed the listener and `ssh.service`
  stayed disabled. With `ssh.service` running instead, the fix reloaded it
  and kept the same main PID, so it did not restart.
- With the only `Include` inside a trailing `Match` block, the fix added a
  top-level `Include` and the hardened values applied globally.
- A lower-numbered drop-in that set `PasswordAuthentication yes` made the fix
  exit 1. The error named the missing `sshd -T` value and the overriding
  drop-in.
- A live `sshd -t` failure after the write restored both files, removed the
  new drop-in, and exited 1. When the restore `mv` also failed, the fix exited
  3 with the "restoring the previous files FAILED" message. No staging or
  restore temp files remained in either case.

On 2026-10-30 the rendered sshd fix before and after the reload change ran as
root in a disposable privileged `ubuntu:24.04` container booted with systemd
255 and OpenSSH 9.6p1, with the serverpro drop-in removed before each run.
Both wrote the drop-in and exited 0 with `ssh.service` masked and stopped (as
OpenSSH off leaves it), running with the socket off (one reload, same main
PID), and inactive behind `ssh.socket`. With `ssh.service` masked while its
sshd still ran, systemd dropped the reload job and reported success, so the
earlier fix left that sshd on its old config; the current fix sent SIGHUP and
sshd logged its restart under the same main PID. On a masked, stopped unit
`systemctl try-reload-or-restart` exits 0, and `reload-or-restart` fails with
"Unit ssh.service is masked."
- When the SSH client was killed during candidate validation with a TTY, the
  hangup trap removed the temp directory and left the live config unchanged.
  Without a TTY the repair received no signal and completed with a valid
  config. A script fed through `sh -s` on a TTY can leak its temp directory
  when killed, but doctor never uses that form because it runs a script file.
- A `failed` `ssh.socket` failed the unit read check. The unit fix reset it
  and started it, the read check passed, and SSH logins worked again.
  `start-limit-hit` could not be reproduced for a socket unit in the
  container.

### Cloudflared check and repair dogfood

The 2026-10-11 cloudflared repair ran the rendered check and repair scripts
with `sh` (dash, as the remote batch and sudo transport do) in a disposable
Ubuntu 24.04 amd64 container. The pinned key and noble source came from the
rendered install script's own helper, which verified fingerprint
`CC94B39C77AE7342A68B89628A682D308D4E5E73`. The old `2026.8.2` `.deb` came from
the `pkg.cloudflare.com` pool, because the signed index lists only the latest
release.

- With no package, the check printed `cloudflared package is not installed`.
- With `2026.8.2` installed, the old check exited 1 with no output, which
  matches the field report. The new check printed
  `cloudflared 2026.8.2 is below minimum 2026.9.3`.
- The repair stopped before any apt call with a "reprovision the server"
  message in four cases: no unit or token, a missing apt source, a source
  without `signed-by`, and a missing keyring. A keyring holding other keys
  failed with a fingerprint mismatch. The package stayed at `2026.8.2`.
- With the provisioned state restored, the repair upgraded to `2026.10.0`.
  The check then printed `cloudflared 2026.10.0 active`.
- A stopped service printed `cloudflared service is not active: failed`. The
  repair restarted it without apt work. A removed package printed
  `cloudflared package status is config-files, want installed`.

The container has no systemd. A `systemctl` stub stood in only for
`is-active`, `enable`, and `restart`, and its `restart` ran the upgraded
binary's `--version` first. Real unit activation was not verified. The live
create journey's `doctor --fix` meets a current connector, so it does not
exercise this repair; confirm it on an affected host. Without the stub, the check printed the
`systemctl: not found` error as the service state. A batched doctor run with
captured output showed the old bare `remote batch command N failed with status
1` evidence now followed by the check's own reason.

### apt package-lists lock wait dogfood

On 2026-10-11 the rendered cloud-init runcmd (through its `apt_update`
call), the managed-package refresh command, the cloudflared upgrade script,
the bootstrap script, and the Tailscale updater ran their `apt_update` helper
against the real `apt-get` (apt 2.8.3) in a disposable Ubuntu 24.04
container. POSIX scripts ran under `sh` (dash) and the bash scripts under
`bash`. A perl `fcntl` write lock on `/var/lib/apt/lists/lock` stood in for
apt-daily.

- With the lock held for 15 seconds, every helper printed apt's
  `Could not get lock /var/lib/apt/lists/lock` error once, then
  `waiting up to 300 seconds for the apt package lists lock`, and returned 0
  after the lock was released (17 to 20 seconds).
- With the budget edited to 6 seconds and a 2-second retry, and the lock held
  for 30 seconds, every helper returned apt's status 100 after about
  8 seconds with apt's error and
  `apt package lists stayed locked for 6 seconds; another apt run still holds them`.
- A malformed sources entry made every helper return 100 within 10 ms with
  apt's `Malformed entry` error and no wait notice. An unresolvable
  repository host is only a warning to `apt-get update` (status 0), so the
  helpers pass it through without retrying.
- With the dpkg frontend lock held instead, plain `apt-get update` returned 0,
  which confirms that update never takes the dpkg lock and that
  `DPkg::Lock::Timeout` cannot cover it.
- In a disposable systemd-booted Ubuntu 24.04 container, the real
  `apt-daily.service` started a full package-list download (throttled only for
  that run) and held the lists lock. The rendered cloudflared install script
  then printed apt's lock error and the wait notice, waited, refreshed, and
  installed cloudflared from `pkg.cloudflare.com`, returning 0 after
  174 seconds. That run outlasts the 2-minute transport default, which is why
  provisioning now runs the install under the shared connector deadline.

### Cloudflared candidate-origin gate dogfood

On 2026-10-11 the rendered repair script's candidate gate ran in a disposable
`ubuntu:24.04` arm64 container with apt 2.8.3. The rendered install helpers
fetched the Cloudflare key (fingerprint verified), wrote the noble source and
the origin pin, and `apt-get update` read `pkg.cloudflare.com`. The candidate
was `2026.10.0`, listed as `2026.10.0 990` with the Cloudflare source at 500.
A local `file:` repository (built with `apt-ftparchive`) stood in for another
configured source.

- Cloudflare only: the gate accepted `2026.10.0`.
- The local repository also offered `2026.10.0`: the gate rejected it.
- The local repository offered only `2099.1.0`, with the pin: apt kept the
  Cloudflare `2026.10.0` as candidate and the gate accepted it.
- The same without the pin: apt chose `2099.1.0` and the gate rejected it.
- Cloudflare source removed, pin kept: apt chose `2099.1.0` and the gate
  rejected it, so the pin alone excludes no source.
- After the repair installed `2026.10.0`, the table showed ` *** 2026.10.0
  990`, the Cloudflare source, and `100 /var/lib/dpkg/status`. The origin
  check accepted it. With the local repository offering the same version
  again, it rejected it.
- With a newer `2099.1.0` from the local repository installed, apt kept it as
  candidate despite the pin and the origin check rejected it. The gate still
  passed, because a connector at or above the floor needs no install. The gate
  did not judge where an installed connector came from. The install receipt
  now closes this gap (see Cloudflared install receipt dogfood).

### Cloudflared install receipt dogfood

On 2026-10-11 the rendered install, repair, and check scripts ran with `sh`
(dash) in a disposable `ubuntu:24.04` arm64 container with apt 2.8.3 and the
real `pkg.cloudflare.com` repository. A second local `file:` repository (built
with `apt-ftparchive`, `trusted=yes`) offered a foreign `2099.1.0` before
provisioning. The check ran as an unprivileged user. A `systemctl` stub
answered `is-active` and ran the binary's `--version` on `restart`. All 32
assertions passed.

- Fresh install: the install chose Cloudflare `2026.10.0` over `2099.1.0`,
  wrote the receipt `0644 root` with no staging file left, and the check
  printed `cloudflared 2026.10.0 active`.
- Healthy host: the repair ran no apt install and left the receipt unchanged.
- `dpkg -i` of the foreign `2099.1.0` (the reported bypass): the check printed
  `cloudflared 2099.1.0 is not the build serverpro installed from
  https://pkg.cloudflare.com/cloudflared`. The repair downgraded to Cloudflare
  `2026.10.0` and the check passed.
- `2099.1.0` installed with apt from the second repository, then the install
  script ran again: the check rejected it and the install restored
  `2026.10.0`.
- A foreign `.deb` with the same version `2026.10.0` (different
  `Installed-Size`): apt listed it as a separate status-only entry. The check
  rejected it on the digest, and the repair's `--reinstall` fetched the
  Cloudflare build.
- `/usr/bin/cloudflared` overwritten in place: the check rejected it and the
  repair restored it.
- Receipt removed, as on a host provisioned before receipts: the check printed
  `cloudflared install receipt is missing: /var/lib/serverpro/cloudflared.receipt`.
  The repair reinstalled `2026.10.0`, wrote the receipt, and the check passed.
- The second repository also offered a different `2026.10.0` while the
  foreign `2099.1.0` was installed: the repair stopped with `no cloudflared
  version is offered only by https://pkg.cloudflare.com/cloudflared` and made
  no package change. With that duplicate removed, the repair succeeded.

Not covered: amd64, and real unit activation under systemd.

### Unmanaged cloudflared audit dogfood

On 2026-10-30 the rendered ingress-`none` check and repair ran with `sh`
(dash) in a disposable systemd-booted `ubuntu:24.04` arm64 container with the
real `pkg.cloudflare.com` repository (candidate `2026.10.6`). The pinned key
and noble source came from the rendered install script's own helpers. The
check ran as an unprivileged user, who cannot read the `0600` token.

- No package: the check and repair printed `cloudflared not installed` and
  exited 0. The repair wrote no apt source and no receipt.
- Dormant (package `2026.6.1` from the pool, unit file `disabled`, service
  `inactive`, no token, receipt for another version): the check printed
  `cloudflared 2026.6.1 is below minimum 2026.10.2`. The repair upgraded to
  `2026.10.6`, wrote the receipt, and printed `cloudflared 2026.10.6 dormant`.
  The unit stayed `disabled`/`inactive` and no connector process ran. The
  recheck passed with the same line.
- A process named `cloudflared` outside the service: the check and repair
  printed `cloudflared is running outside cloudflared.service`; the package
  stayed at `2026.6.1`.
- Active (custom unit, `0600` token, enabled and running, `2026.6.1`, no
  receipt): the check printed the below-minimum reason. The repair upgraded
  to `2026.10.6` and restarted the service; its journal showed the new
  binary's version. The recheck printed `cloudflared 2026.10.6 active`. The
  token kept mode `0600` and its contents.
- Real systemd reported a missing unit as `not-found` with an empty
  `UnitFileState`, and `dpkg-query` exited 1 for an unknown package, as the
  classifier expects. Without D-Bus, `systemctl` fails for a non-root user,
  so the unprivileged check failed closed with `systemctl show cloudflared
  failed` until D-Bus was installed. Ubuntu servers ship D-Bus, and the
  doctor batch runs under sudo.

Not covered: amd64, a `half-installed` package on a real host, and a real
tunnel token (the active unit ran the binary's `--version` and then slept).

### Drift checks dogfood

On 2026-10-30 a build with the drift checks ran `serverpro server doctor
--full` without `--fix` against managed Ubuntu 24.04 servers on DigitalOcean
and Vultr.

- Every drift probe ran in the doctor batch and exited 0. Each check gave a
  verdict, and none reported that the read-only probe could not run.
- `metadata user-data` read `/metadata/v1/user-data` on DigitalOcean and
  `/latest/user-data` on Vultr. Its evidence held no user-data, hash, or
  shadow entry.
- `cloudflared ingress` ran only on servers with ingress `none`, where a
  dormant cloudflared package passed. Servers with ingress
  `cloudflare-tunnel` ran `cloudflared apt pin` instead.
- `tailnet policy` read the policy file with the stored Tailscale API token.

Not covered: Hetzner, whose metadata path has no live read.

A later run rendered the `privileged accounts` probe before and after the
listed-command resolution and ran both with `sh` (dash, mawk, GNU
`realpath`) as root in a disposable `ubuntu:24.04` container. Drop-ins
listed a symlink to bash, a symlink to a GNU-prefixed compiler, a script in
a group-writable directory, a script another user owns, a world-writable
script, a missing path, `sqlite3` and `editor` by name, and `/usr/bin/id`.
The old probe only warned on all of them. The new one failed each one and
named why, and still only warned on `/usr/bin/id`.

Another run rendered the `tailscale serve` probe before and after it held
both reads until both succeeded, and ran both with `sh` (dash) in a
disposable `ubuntu:24.04` container with fake `tailscale` and `ss`. When
`ss` failed, the old probe had already printed the full Serve config, which
then went into the doctor evidence. The new probe printed only the failed
command and its exit status, for a failed `ss` or a failed
`tailscale serve status`. With both reads passing, the two printed the same
output.

### Restricted egress lockdown dogfood

On 2026-10-30 the rendered lockdown and the `egress restricted` probe ran with
`sh` (dash) as root in a disposable `ubuntu:24.04` arm64 container with the
real `ufw` and `iptables` packages and `NET_ADMIN`.

- Cloud-init baseline (outgoing allowed): the probe reported the `allow`
  default, all 8 missing lockdown rules, and the open `1.1.1.1:853` connect.
- The lockdown exited 0, reported the `deny` default, the blocked `853`
  connect, and the allowed `controlplane.tailscale.com:443` connect. The probe
  then passed. A second run was idempotent.
- A deleted `ALLOW OUT 53` rule and a default flipped back to `allow` were
  each reported, and each was repaired by the next lockdown run.
- An `iptables` reject on 443 made the lockdown exit 1 with the allowed
  connect failure. An inactive UFW made it exit 1 with the unset default.

Not covered: a real provider host (doctor `--fix` was not run against
production) and Docker's own `DOCKER-USER` rules.

### Provider image account dogfood

On 2026-10-30 the rendered image account removal and the
`privileged accounts` probe ran with `sh` (dash) as root in a disposable
`ubuntu:24.04` arm64 container with `sudo` and `procps`. The host had an
admin user, a `linuxuser` image account in `sudo` with its own sudoers.d
file, home keys, and a crontab, a `deploy` account with its own sudoers.d
file, and a sudoers.d file with rules for both.

- The probe reported the image account, its group and sudoers.d grants, the
  `deploy` grant, both rules in the shared file, and the image's own `ubuntu`
  account in `sudo`. It did not report the admin user.
- The removal archived the home (with `.ssh/authorized_keys`), the crontab,
  and the image account's sudoers.d file, kept the shared file, passed
  `visudo -c`, and removed the account. The probe then still reported
  `deploy`, the shared file, and `ubuntu`. A second run was a no-op.
- With a process running as the account, the removal stopped before any
  change. With an invalid sudoers.d file present, `visudo -c` failed and the
  removal moved the image account's file back and stopped.

A second run on 2026-10-30, in the same kind of container with `sudo` and
`procps`, compared the earlier script with the current one. The image
account had its own and a wrapped rule in a sudoers.d file that also had a
`deploy` rule.

- Earlier script: it kept the shared file whole and removed the account. A
  new account with the same name then had all the old sudo rules
  (`sudo -l -U`). It did the same with a `User_Alias` that named the
  account.
- Current script: it removed only the image account's rules (the wrapped
  rule as a whole), archived the original, passed `visudo -c`, and removed
  the account; a new account with the same name had no sudo. With the
  `User_Alias`, it stopped before `userdel` and left the file unchanged.
  With only a comment naming the account, it left the file unchanged and
  removed the account. With an invalid second drop-in, the live
  `visudo -c` failed, and it put the original file back unchanged and
  stopped. With a process running as the account, it stopped after it
  locked and expired the account, and the undo command in its message gave
  back the password, the expiry, and the shell.

A third run compared the script before and after the check of sudoers
files outside `/etc/sudoers.d`, in the same kind of container. With a rule
for the image account in `/etc/sudoers`, in an `@include` file, or in an
`#includedir` file, the earlier script removed the account, and a new
account with the same name got that rule (`sudo -l -U`). The current script
stopped before any change: the account kept its shell, expiry, and home,
and its sudoers.d file stayed. With a broken drop-in it stopped before any
change too, where the earlier script had locked the account first. With
only its own drop-in, both removed the account.

A fourth run made one tool fail at a time with an exit other than "no
match" (`pgrep` 3, `grep` 2 on the drop-ins, `awk` 2, `getent passwd` 3).
The earlier script read each failure as "nothing found": it removed an
account whose processes it could not list, left a `NOPASSWD: ALL` drop-in
for a later account with the same name, archived a shared drop-in with
another account's rule, and removed drop-in rules for an account it then
called absent. The current script stopped on each one, before `userdel`,
and the other account kept its sudo rule.

On 2026-10-30 the `privileged accounts` probe and the image account removal
moved from line patterns to sudo's own parser (`cvtsudoers` JSON). Both
were rendered before and after the change and run as root in a disposable
Ubuntu 24.04 arm64 container with sudo 1.9.15p5, one sudoers sample per
case, each accepted by `visudo -c` first. Doctor's own grader graded every
probe output.

- The old probe passed or only warned on: a `!!`-negated shell, a rule
  continued by a backslash and trailing blanks, a `#uid` rule, the user
  lists `admin , other`, `root ,other`, and `%sudo ,other`, a `SETENV` tag,
  `Defaults` that keep `LD_PRELOAD` and `PYTHONPATH`, turn off
  `env_reset`, or turn on `setenv` for a run-as binding, an unlimited rule
  in an `@include` file outside sudoers.d, a symlink hop through a
  world-writable directory, a symlink another user owns, and `sort` and the
  dynamic loader listed by path. The new probe failed each one and named
  why.
- A symlink chain ending at bash and an escaped-space path with a file of
  the shorter name failed in both; the new probe names the bash target and
  the ambiguous path.
- Only listed commands, through a `Cmnd_Alias` or in `@include` and
  `#include` files, still only warned. A host with only the admin rule
  passed.
- A broken drop-in and a missing `cvtsudoers` each failed the check as
  unreadable; the old probe passed the second.
- `visudo` rejects a backslash in a command path, so no valid sudoers lists
  one; the grader still fails such a path.

The removal ran once per case in a fresh container, then the account
database and `cvtsudoers` showed what was left.

- With a `#uid` rule for the account, a rule in an include named by a
  nested sudoers.d path or a `..` path, or a user list `account , other`,
  the old script removed the account. It left the rule live, or dropped the
  other account's grant with it. The new script stopped before any change,
  or at the shared file for the user list.
- With a name only sudo's parser reads (`linux\user`), the old script
  removed the account and left a live rule. The new one stopped before
  `userdel`, with the account locked and the undo command in its message.
- With a rule continued past trailing blanks, the old script stopped on a
  candidate that `visudo` rejected. The new one removed the whole rule and
  kept the other account's.
- With another account's `#uid` rule in the same drop-in, the old script
  archived the whole file and that rule with it. The new one kept the rule.
- With only its own drop-in, both removed the account.

A fifth run ran the undo command from the "running processes" stop, in the
same kind of container, for four starting states. The earlier command
cleared an expiry date the account had, unlocked a password that was locked
before the removal, and failed with `invalid shell` for an empty shell
field, which left the account locked and expired. The current command put
back the password lock state, the expiry, and the shell exactly in all four
states, and its quoting kept a value with a single quote as one word.

Not covered: a real Vultr host (doctor `--fix` was not run against
production).

### Unattended-upgrades drop-in dogfood

On 2026-10-30 the rendered `unattended upgrades config` probe and repair, and
the `reboot pending` probe, ran with `sh` (dash) as root in a disposable
`ubuntu:24.04` arm64 container with `unattended-upgrades`
`2.9.1+nmu4ubuntu1`.

- On the stock package the probe warned that
  `Unattended-Upgrade::Remove-Unused-Kernel-Packages` is not set. The repair
  wrote the drop-in (`0644`, no temporary file left), and the probe then
  passed. A second repair was a no-op.
- With a later apt.conf.d file setting the option to `false`, the probe
  failed, and it still failed after the repair, as designed. With the drop-in
  itself edited to `0`, the repair restored it and the probe passed.
- `apt-config dump` and `unattended-upgrade --dry-run --debug` read the
  drop-in.
- With `/var/run/reboot-required` and its package list present, the reboot
  probe warned with the packages; without them it passed.

Not covered: a real kernel update and old-kernel removal (the container has
no kernel packages), and a real host (doctor `--fix` was not run against
production).

### Gated reboot dogfood

On 2026-10-30 the rendered gated reboot install script and the
`reboot survivability` probe ran as root in a disposable `ubuntu:24.04`
container that boots systemd 255, with `docker.io` (Docker 29.1.3), `tmux`,
`netcat-openbsd`, `needrestart`, logind, and a second user. The container
ran with a restart policy, so `systemctl reboot` from inside it stopped
systemd and Docker started it again with a fresh `/run` and the same
`/var/lib`. The probe output was graded by doctor's own result code. The
verify timeout was `1m` to keep the runs short.

- The install script enabled the timer (next run at 02:00 UTC) and the
  verify unit; a second run changed nothing. With a reboot pending it
  warned. With the script at mode `0666`, an edited timer, and the timer
  disabled, the probe failed with four `install:` findings and doctor marked
  them fixable; the install script repaired all four and the probe passed.
  An older installed script was reported as changed.
- Clean host: the preflight exited 0, the window wrote the snapshot (five
  enabled units, two containers with restart policy `unless-stopped`, one of
  them with a health check, and TCP port 8090), and rebooted. After boot
  `/var/run/reboot-required` was gone, the verify unit ran after
  `docker.service` while boot finished, and wrote `status=ok`. The state
  directory, created by the gate, was `0700` and its files `0600`, owned by
  root.
- Containers with restart policy `no` and `on-failure`: the preflight
  exited 3 with one gap for each and the operator command; the
  `unless-stopped` container was not reported. With a reboot pending, the
  window logged both gaps and `reboot-skipped`, wrote no snapshot, and did
  not reboot. Doctor reported a warning that is not fixable.
- A `nc` listener started in `tmux` by the second user ran in a
  `tmux-spawn-*.scope` of that user's manager; the preflight reported it and
  the window skipped the reboot. A `nohup` listener in the login session
  scope and a `systemd-run --user` service of a user without linger were
  reported too; with linger the user service was not.
- Units that fail after boot: an enabled unit that fails its first start
  recovered after the one restart. An enabled unit that fails at every
  boot got one restart and was reported as not active. An enabled unit that
  was stopped before the reboot and failed at boot was reported as failed
  and never restarted by the check. The result was `status=degraded`, the
  verify unit exited 1, and doctor warned with the findings.
- With `reboot.enabled: false` in config and the timer still enabled,
  doctor warned and named `systemctl disable --now`.
- A Docker daemon restart brought back an `on-failure` container that was
  killed at shutdown, but not one that exited 0 on `SIGTERM`.

A second run on the same day covered the fail-closed and snapshot rules. A
container shares the host kernel's boot ID, so a throwaway unit in the
container bound a fresh ID over `/proc/sys/kernel/random/boot_id` at every
guest boot.

- Static units: a static unit started by hand was a gap. A static unit
  wanted by a running enabled service and the stock static services
  (`dbus`, `systemd-journald`, `systemd-logind`) were not. This run also
  passed a static unit for its D-Bus name alone; the gate no longer trusts a
  bus name without an activation file (see the D-Bus activation run below).
- With Docker `live-restore` on and `docker.service` stopped, two
  containers kept running in their scopes. The preflight exited 3 with one
  gap that named both and `systemctl enable --now docker.service`, and
  doctor warned.
- A snapshot two hours old from another boot, and a snapshot from the
  running boot: verify wrote `status=stale` with the reason, retired the
  snapshot, exited 1, and did not start the stopped unit in the snapshot.
  Doctor warned with the reason.
- A snapshot with no check running: doctor warned. A verify unit that
  failed in the running boot with only an older result on disk: doctor
  warned.
- `systemctl stop` on a running verify: the result was `status=degraded`
  with "stopped before it finished", the snapshot was retired, and no unit
  was started.
- `reboot.target` masked for the run: the window wrote its snapshot, then
  `systemctl reboot` failed, the window removed the snapshot, logged the
  error, and failed.
- Clean host, real guest reboot: the boot ID changed, the snapshot passed
  the boot and age checks, and the result was `status=ok`.
- Units that fail after boot, rerun with a real guest reboot: the same
  results as the first run, and doctor warned with both findings. An
  enabled unit that was not running before the reboot and failed at boot
  was reported as failed and not started.
- Containers with restart policy `no` and `on-failure`, rerun: the same two
  gaps and exit 3.

A D-Bus activation run on 2026-10-30 rendered the installed gate script and
ran its preflight as root in the same systemd 255 container. Each case wrote
its own test unit and system bus activation file under
`/usr/share/dbus-1/system-services`; no `dbus-daemon` ran, because the
preflight reads the files and never asks the bus. Only the gap line for the
test unit was graded, since the container's own gettys are always gaps.

- A static unit with a `BusName=` and no activation file, started by hand,
  was a gap, as was a static unit with no bus name started by hand.
- A static unit whose activation file named it in `SystemdService=` was not
  a gap, and neither was one whose file named its `dbus-org.*` alias, the
  way `systemd-timedated` is named.
- A unit masked while it ran was a gap with its activation file in place.
- The same run against the rule before `SystemdService=` was checked
  passed four units that do not come back: one whose activation file named
  another unit, one whose file had no `SystemdService=` (dbus-daemon would
  run its `Exec=` line itself), one whose matching file in
  `/usr/share/dbus-1/system-services` was shadowed by a file for the same
  name in `/usr/local/share/dbus-1/system-services` that named another unit,
  and a `systemd-run` unit (`UnitFileState=transient`, `Transient=yes`)
  whose file named it. The current rule reports each as a gap and still
  passes the id and alias cases above.

Not covered: a real VM reboot, a real kernel update, and a real host
(doctor `--fix` was not run against production).

### Provider error evidence dogfood

On 2026-10-30 a throwaway harness, never committed, ran the OpenSSH
preflight's tunnel read (`CheckTunnelSSHIngress`) through the real Cloudflare
client against a fake API (`httptest`), with the old (before this change)
and new code. With an API `500` and a 2 KiB body, the old error text was
2330 bytes and held the account ID, the tunnel ID, and the whole body; the
new text was 431 bytes, named `/accounts/{id}/cfd_tunnel/{id}/configurations`,
kept the error code and message, and ended with the body's full length. With
the API unreachable, the old transport error repeated both IDs in the URL;
the new one masked them and kept the connection error.

A second harness ran delete's tunnel ownership check
(`validateTrackedTunnel`) through the real Cloudflare client against a fake
API that answered with another tunnel's UUID. The old error printed the
live and the state tunnel IDs; the new one printed
`live tunnel id "{id}" state "{id}"`.

### Tailnet policy allow-all dogfood

On 2026-10-30 a throwaway harness, never committed, ran the provider
`tailnet policy` check through the real Tailscale client against a fake API
(`httptest`) that served HuJSON policy files, with the old (before this
change) and new code.

- Old: only the literal default grant warned. A grant of `tcp:*` or `udp:*`
  to every host, a source of `100.64.0.0/10` or `fd7a:115c:a1e0::/48`, a
  destination of `autogroup:tagged` in a grant or an acl, and a `hosts`
  alias for the tailnet range or for `0.0.0.0/0` all passed.
- New: each of those warned and named the rule, with an alias shown as
  `tailnet (100.64.0.0/10)`. A least-privilege policy (`tcp:22` and
  `tcp:443` to the server tag), `100.64.0.0/11`, `icmp:*`, and an alias for
  one address still passed.

Not covered: the real tailnet policy file, and a custom IP pool narrower than
`100.64.0.0/10`, which a CIDR could cover without covering the whole range.

A second run on 2026-10-30 used the same harness shape for
`autogroup:danger-all` and IP sets.

- Old: a grant and an acl from `autogroup:danger-all` to every host on every
  port passed, and so did grants and acls with an IP set as the source or the
  host (`ipset:all:*` in an acl), a nested set among them.
- New: each of those warned, and an IP set showed as
  `ipset:all (an IP set doctor does not resolve, counted as every address)`.
  `autogroup:danger-all` on `tcp:22` only, an IP set source to the server tag,
  an IP set on `tcp:22` or `ipset:prod:22`, and a least-privilege policy
  still passed.

Not covered: the add and remove lines of a real IP set, since doctor does not
evaluate them.

### Tunnel SSH route preflight dogfood

On 2026-10-30 two throwaway harnesses, never committed, ran the old (before
this change) and new code against a fake Cloudflare API (`httptest`):

- `doctor --fix` (`RunWithOptions` with a fake host whose `openssh disabled`
  check fails). Old: the repair ran for every ingress, and doctor never
  requested `/configurations`. New: the repair did not run for an `ssh://`
  service, `tcp://127.0.0.1:22`, `localhost:22`, bastion mode, `socks5` (a
  name cloudflared does not have; see the allowlist run below), or
  an `ssh://` URL with a password, nor on an API `500`; the finding said
  `fix not run` and named the service with the password masked, never the
  hostname. An `http://localhost:8080` route and a tunnel without remote
  configuration (`config: null`) let the repair run, and the check passed.
- Create's OpenSSH-off step. Old: the host script ran with an `ssh://` route
  and on an API `500`. New: it did not run in either case, and it ran for a
  fresh tunnel (`config: null`).

A later throwaway harness ran the old denylist and the new allowlist
classifier on the same ingress services. The old one passed `socks-proxy`
(cloudflared's real SOCKS service), `rdp://` and `smb://` origins, a `tcp`
origin without a port, a rule without a service, and any proxy type other
than `socks`. The new one fails all of them, and every service it cannot
classify (`localhost:8080`, `warp-routing`, an unparsable URL) under a fixed
label that prints no value. Both pass `http://localhost:8080`,
`https://localhost`, `tcp://127.0.0.1:5432`, `unix:` sockets, `hello_world`,
and `http_status:404`.

A third harness on 2026-10-30 ran the remote half
(`CheckTunnelSSHIngress`) through the real Cloudflare client against a fake
API that answered the configuration and the two private route lists.

- Old: a configuration with `warp-routing` on and no routes stopped the
  step, while a private network route, a private hostname route, a `403` on
  either list, and a list answer without `result` all passed, because the
  old code never read the lists.
- New: the `warp-routing`-only configuration passed; a network route and a
  hostname route each stopped the step and were named by a fixed label; a
  `403` stopped it and named the `Cloudflare Tunnel Read` permission; an
  answer without `result` stopped it. Each list request carried
  `tunnel_id`, `is_deleted=false`, and `per_page=1`, and no message held the
  tunnel ID.

Not covered: the real Cloudflare API, since serverpro has no throwaway
Cloudflare account to add a private route to.

The rendered old and new host scripts then ran with `sh` (dash) as root in a
disposable `ubuntu:24.04` container that boots systemd, with
`openssh-server` and `iproute2`, a dummy `tailscaled` unit, and a fake
`tailscale` CLI. A fake token string stood in for the tunnel token in every
cloudflared setting. The old script masked `ssh.service` in every case. The
new script stopped and left `ssh.service` unmasked for a stopped
`cloudflared.service` with `--url ssh://localhost:22`, a running
`cloudflared-extra.service` with `--url=tcp://127.0.0.1:22`, a
`TUNNEL_URL=localhost:22` environment, and `/etc/default/cloudflared` with
`--url ssh://localhost:22`. It masked `ssh.service` for no cloudflared at
all, a running managed-shape unit with only a token (its `ExecStart` read
shows start times such as `17:08:46`), and `--url tcp://localhost:2222`. No
run printed the token.

A later run compared the host scan before and after it followed cloudflared's
own config search, with `sh` (dash) as root in a disposable `ubuntu:24.04`
container with fake `systemctl`, `tailscale`, `ss`, and `journalctl`. Both
scripts turned OpenSSH off with no cloudflared config, and with a web-only
ingress in `/etc/cloudflared` and a login certificate in `/root/.cloudflared`.
Both stopped for an SSH route in `/etc/cloudflared`. The old script turned
OpenSSH off, and the new one stopped, for an SSH route in
`/usr/local/etc/cloudflared`, in `/root/.cloudflared`, in another user's
`~/.cloudflare-warp`, or in a config symlinked into `/etc/cloudflared`; for a
unit `--config` (or a `--config` in an environment file variable) that names
a config with an SSH route; for a `--config` that is missing or relative;
for `TUNNEL_URL` on port 22 in a unit's `EnvironmentFile`; for a default
config path that grep could not read; and for a `cloudflared` process outside
the units. A read-only check on a managed server placed its running
`cloudflared` in `cloudflared.service`, which the scan accepts.

On 2026-10-30 the host scan moved from a denylist to the remote ingress
allowlist. The rendered old and new scripts ran with `sh` (dash) and `mawk`
as root, once per case in a fresh `ubuntu:24.04` arm64 container with fake
`systemctl`, `tailscale`, `ss`, and `journalctl`, and real `pgrep`,
`getent`, `find`, and `readlink`. A fake token stood in for the tunnel token
in every unit and environment file; no run printed it.

- The old script turned OpenSSH off, and the new one stopped, for a config
  service `tcp://localhost:0022`, `http://localhost:022`, or
  `https://127.0.0.1:00022`; `rdp://localhost:3389`; the `bastion` and
  `socks-proxy` services; `bastionMode: true` in the configuration-wide
  `originRequest`; a flow-style `proxyType: socks`; top-level
  `bastion: true` or `socks5: true`; `url: localhost:8080` with no scheme; a
  service on the next line; and a folded block scalar. It stopped too for a
  unit `--bastion`, `--url tcp://localhost:0022`, `TUNNEL_SOCKS=true` or a
  quoted `TUNNEL_URL` to `rdp://` in an `EnvironmentFile`, an SSH-capable
  config under a `HOME` the unit sets outside the account database, a
  relative unit `HOME`, and a `cloudflared` binary run through a symlink with
  another name, which `pgrep -x` misses.
- Both turned OpenSSH off with no config, a web-only config, a JSON-style
  and a YAML flow-style ingress on allowed ports, a commented-out `rdp://`
  service, a quoted `#` fragment, a unit `--url=http://localhost:8080`, and
  `TUNNEL_BASTION=false`.
- The awk program alone, without the grep in front of it, failed each port
  22 spelling (`0022`, `022`, `00022` in brackets, an uppercase scheme, a
  URL with a password), a `tcp` origin without a host or port, `ssh://`, an
  escaped quote, an uppercase `Service:` key, `proxyType:` with no value,
  `bastionMode: no`, a port above 65535, a bare `--socks5`, and
  `--url ${ORIGIN}`. It passed `http://localhost`, `https://localhost:8443`,
  `unix:`, `hello_world`, `http_status:404`, `proxyType: ""`,
  `bastionMode: false`, `TUNNEL_BASTION=`, `--socks5=false`, and a longer
  name such as `--url-other` or `X_TUNNEL_URL`.
- The remote check now reads ports the same way: `tcp://localhost:0022`,
  `http://localhost:022`, `https://127.0.0.1:00022`, a 20-digit port, and
  port 70000 count as SSH routes; `tcp://localhost:2222`,
  `http://localhost:8080`, and `https://localhost` do not.

### Unit-test cases moved to harness runs

On 2026-10-30 the cases a review range had added to the unit tests as shell
scripts against fakes, or as Go rows the coverage gate does not need, moved
out of the unit tests:

- The sshd settings fix on a masked `ssh.service`, stopped or running, and
  the `tailscale serve` probe with a failed `ss`. Real systemd and the
  rendered probe in an Ubuntu 24.04 container prove them, as recorded under
  sshd repair fail-safe dogfood and Drift checks dogfood.
- The Serve check's evidence when the SSH run fails after the probe printed
  the config, and output without the listener section; nine packet-policy
  rows (CIDR, member, protocol, port-list, tailnet-range, tagged, and
  `hosts` alias spellings, flagged and not flagged); the status error's ID,
  query, and length handling; and the tunnel ingress read's
  configuration-wide defaults, empty body, and foreign tunnel.

A throwaway harness, never committed, ran each moved Go case through the
real code path (the Tailscale and Cloudflare clients and `httpjson.Client.Do`
against a fake API, the Serve check with a scripted remote, and the OpenSSH
preflight). All 26 checks passed with the same results the removed rows
expected; the ingress cases also stopped the preflight (bastion mode twice,
then an unread configuration for the empty and the foreign answer).

Coverage of the four affected packages went from 90.6% to 89.6% of
statements, and the repository aggregate from 85.9% to 85.7%, with no
function at 0%. The gate (81.8%) still passes, so no Go row stayed.

### OpenSSH off dogfood

On 2026-10-30 the rendered OpenSSH-off step and the `openssh disabled` probe
ran with `sh` (dash) as root in a disposable `ubuntu:24.04` arm64 container
that boots systemd, with `openssh-server` `1:9.6p1-3ubuntu13.20`. A dummy
`tailscaled` unit, a fake `tailscale` CLI that prints prefs, and a dummy
`droplet-agent` unit stood in for the real daemons.

- On the stock host the probe failed: `ssh.socket` enabled and active, and
  `systemd` listening on port 22. It warned that `ssh.service` is not masked
  and that the flag file is missing.
- The step stopped and changed nothing (units, state file, and flag file
  unchanged) for each condition: `tailscaled` inactive, `RunSSH` false, a
  cloudflared ingress to `ssh://localhost:22`, a recent ISO `auth.log` login,
  a gzipped classic-format `auth.log` login, an established OpenSSH session,
  and the journal login of that session. The messages named only the method
  and the user. An ISO `auth.log` login from 40 days ago and an ingress to
  port 2222 did not stop it.
- With the checks clear, the step warned about the admin user's locked
  password and the active `droplet-agent`, wrote the state file (`0600`, in a
  new `0700` directory) with `ssh.socket=enabled`, `ssh.service=disabled`, and
  `condition=absent`, masked both units, created the flag file, and left no
  listener on port 22. The probe then passed. A second run kept the first
  record.
- On the masked host without `/run/sshd`, `sshd -T` failed with the missing
  privilege separation directory, and all nine sshd hardening reads passed
  through the drop-in fallback. The `sshd units` read passed, its repair
  started nothing, and the sshd settings repair created `/run/sshd` and left
  both units masked and inactive.
- `apt-get install --reinstall openssh-server` left both units masked and
  inactive.
- Drift: a missing flag file and an unmasked but disabled unit warned. With
  both units unmasked, the flag removed, and the socket enabled and started,
  the probe failed, and the step converged the host again.
- The rollback in USAGE.md (unmask, remove the flag, enable the units that
  the state file records as enabled) restored `ssh.socket`, and an OpenSSH
  login worked. That login then stopped the next run of the step, as
  designed.

Not covered: a real host with a real `tailscaled` (doctor `--fix` was not run
against production, and no dogfood server exists), and the provider consoles.

## Command surface matrix

| Command | Capability | Unit/integration proof | Smoke/e2e proof | Dogfood proof |
| --- | --- | --- | --- | --- |
| `serverpro` | Root command, JSON default, version, timeout parsing, scoped global flags. | `internal/cli/root_test.go` | `scripts/test-cli-no-token-surface.sh` help/version/timeout cases | Read-only dogfood |
| `serverpro namespace` | Namespace parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro namespace create` | Create namespace config/state dirs with private permissions; dry-run preview; serialize against namespace server work. | `internal/cli/namespace_command_test.go` | namespace create + mode checks | Read-only dogfood |
| `serverpro namespace list` | Deterministic namespace inventory JSON. | `internal/cli/namespace_command_test.go` | empty and populated list cases | Read-only dogfood |
| `serverpro namespace status` | Namespace lookup and server counts. | `internal/cli/namespace_status_command_test.go` | status/missing namespace cases | Read-only dogfood |
| `serverpro namespace delete` | Preview, confirmation, exclusive namespace authority, post-lock registry plus parsed-state/missing-status revalidation, conditional stale cleanup, server cleanup delegation, local namespace removal. | `internal/cli/namespace_delete_command_test.go` | compiled dry-run plan plus no-write proof | Add live namespace cleanup only after server delete dogfood is stable |
| `serverpro server` | Server parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro server create` | Provider-agnostic create flow: explicit catalog selections, supported-image and location-offered size preflight before any Tailscale or provider mutation (size check skipped when resuming a server state already records), config completion, credentials, sudo password hashing, stored sudo/PAT reuse and storage gating (`internal/cli/sudo_password_test.go`), lifecycle, state checkpoints, stderr progress, doctor. | `internal/cli/create*_test.go`, `internal/cli/choices_test.go`, `internal/lifecycle/provision*_test.go`, `internal/cli/sudo_password_test.go`; provisioning and doctor use independent timeout budgets, live catalog filtering rejects unsupported OS/release/architecture entries, preflight rejects missing/unsupported selected images before mutation, config parsing uses the approved byte snapshot, conditional publication rejects appearance/removal/edit drift under coordinated file locking, namespace deletion is excluded, and token-relative policy work excludes explicit reconciliation | dry-run create plus hermetic compiled full-chain journeys for Hetzner, Vultr, and DigitalOcean; `internal/e2e/tailnet_identity_test.go` stale-twin bind and ambiguous-twin fail-closed journeys with no Tailscale SSH call; checkpoint-failure recovery, GitHub PAT stored only after the remote accepts it with the rejected value never persisted, credential merges that preserve a concurrent rotation | Live dogfood optional create→delete |
| `serverpro server bootstrap` | Re-run managed host tools on existing hosts for `all`, `git`, `docker`, `mise`, `node`, `pi`; emit stderr progress; the Git target converges Git/OpenSSH, target-user mise, and gh before persisted `none`, single-repo deploy-key, or PAT-required account-key setup. | `internal/cli/bootstrap*_test.go`, `internal/cli/git_identity_test.go`, `internal/bootstraptools/*_test.go`, `internal/lifecycle/git_deploy*_test.go`, `internal/lifecycle/git_identity*_test.go`; intent-before-mutation, required-PAT, exact doctor state, deploy-to-account cleanup, key rerun/public-key repair | dry-run all targets with fixture state | Live dogfood optional idempotent target after create; interactive GitHub registration remains manual |
| `serverpro server list` | Local registry/state listing with namespace/provider filters. | `internal/cli/server_read_command_test.go` | fixture list cases | Read-only dogfood |
| `serverpro server status` | State resolution, ambiguity handling, provider status refresh, IP update, JSON row. | `internal/cli/server_discovery_command_test.go`, `internal/cli/server_read_command_test.go` | no-token credential guard and `--all` cases | Live dogfood after create |
| `serverpro server doctor` | Local, provider, Tailscale, Cloudflare, SSH, blocking remote platform authority, remote hardening, sudo, exact tool pins, refreshed apt package freshness, and redaction checks; one snapshot per service, one read-only remote batch, remote-only sudo retry, optional package/tool/Tailscale fix path. | `internal/cli/server_doctor_command_test.go`, `internal/doctor/*_test.go`; unsupported platforms disable every sequential/batched fix, `--fix` refreshes package metadata before trusting a cached-current result, package and fixable-check failure evidence retains command output and terminal repair errors within redacted UTF-8 bounds, and ingress-enabled `--fix` plans and runs the cloudflared repair, while ingress `none` plans only the unmanaged cloudflared audit and repair under the same check name | dry-run/guard cases plus production-orchestrated compiled full-chain reports and package-failure journeys without/with `--fix` | Live dogfood after create |
| `serverpro server ssh` | Tailscale SSH target construction, admin user resolution, atomic config recovery update, dry-run command, live shell handoff. | `internal/cli/server_read_command_test.go`, `internal/config/io_test.go`, `internal/remote/*_test.go` including fake `tailscale` process | dry-run fixture case | Manual/live session only; do not automate interactive shell by default |
| `serverpro server discover` | Read provider inventory and filter serverpro ownership labels. | `internal/cli/server_discovery_command_test.go`, `internal/importsync/discover_test.go` | compiled help, provider validation, and scrubbed-token guard cases | Live read-only API dogfood |
| `serverpro server import` | Rebuild config, credentials, state, registry; valid provider-only mesh intent; partial credential completion; optional Tailscale and Cloudflare enrichment; preserving force refresh and service-token merge; dry-run/force/all/provider-id gates. | `internal/cli/server_import_command_test.go`, `internal/importsync/import_test.go`, `internal/importsync/enrich_test.go`, `internal/credentials/save_test.go` | compiled provider-only import → doctor dry-run → SSH recovery journey; help, argument/provider validation, scrubbed-token dry-run guard, and no-write proof | Live read-only discover first; import live only into isolated HOME when explicitly requested |
| `serverpro server start` | Ownership-checked provider power-on. | `internal/cli/server_power_delete_command_test.go`, provider adapter power tests | dry-run fixture case | Live dogfood optional after create |
| `serverpro server stop` | Ownership-checked graceful shutdown. | `internal/cli/server_power_delete_command_test.go`, provider adapter power tests | dry-run fixture case | Live dogfood optional; avoid stopping shared/manual fixtures |
| `serverpro server restart` | Ownership-checked reboot. | `internal/cli/server_power_delete_command_test.go`, provider adapter power tests | dry-run fixture case | Live dogfood optional after create |
| `serverpro server delete` | Destructive preview, confirmation, authority-drift rejection, read-only external preflight, ownership validation, compute delete, partial-failure recovery evidence, external cleanup, and guarded purge of canonical config, credentials, state, import markers, and server locks while preserving custom config and state paths. | `internal/cli/server_power_delete_command_test.go`, `internal/cli/server_delete_cleanup*_test.go`, `internal/cli/server_delete_preflight_test.go`, `internal/cli/server_delete_local_test.go`, provider delete tests; invalid external credentials stop before compute mutation, post-compute cleanup failure retains authority, canonical cleanup serializes with managed writers, local cleanup failure retains registry authority, and pre-lock registry replacement cannot redirect the approved state target | dry-run fixture cases plus `internal/e2e/delete_failure_test.go` compiled failure/retry journeys and production-orchestrated full-chain canonical artifact cleanup | Live dogfood cleanup path |
| `serverpro provider` | Provider parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro provider list` | Registered provider inventory. | `internal/cli/provider_catalog_command_test.go` | provider list case | Read-only dogfood |
| `serverpro provider status` | Provider capability JSON. | `internal/cli/provider_catalog_command_test.go` | status for Hetzner, Vultr, DigitalOcean | Read-only dogfood |
| `serverpro provider doctor` | Ephemeral token validation against provider APIs. | `internal/cli/provider_catalog_command_test.go`, provider adapter doctor tests with fake HTTP/clients | non-interactive token guard | Live read-only API dogfood |
| `serverpro location` | Location parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro location list` | Provider location inventory via ephemeral token. | `internal/cli/provider_catalog_command_test.go`, provider catalog mapping tests | token guard for every provider | Live read-only API dogfood |
| `serverpro size` | Size parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro size list` | Provider size/plan inventory filtered by location. | Provider catalog mapping tests | token guard for every provider | Live read-only API dogfood |
| `serverpro image` | Image parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro image list` | Provider image inventory filtered by location where supported. | Provider catalog mapping tests | token guard for every provider | Live read-only API dogfood |
| `serverpro server ingress` | Ingress parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro server ingress add` | Add pending Cloudflare Tunnel route metadata without false public-route success claims. | `internal/cli/ingress_command_test.go`, `internal/ingress/ingress_test.go` | fixture add/duplicate/error cases | Read-only dogfood |
| `serverpro server ingress list` | List local ingress state. | `internal/cli/ingress_command_test.go` | fixture list cases | Read-only dogfood |
| `serverpro server ingress remove` | Remove local route metadata through adapter. | `internal/cli/ingress_command_test.go`, `internal/ingress/ingress_test.go` | fixture remove/error cases | Read-only dogfood |
| `serverpro tailnet` | Tailnet parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro tailnet reconcile` | Conservatively remove unused serverpro ACL entries using stable matching-tailnet state, live-device evidence, retained tag-owner dependencies, and an unchanged approved plan. | `internal/cli/tailnet_reconcile_command_test.go`, `internal/provider/tailscale/policy_reconcile_test.go` cover explicit identity, shared locking, scoped evidence, owner-reference retention, stderr preview/one stdout document, changed-plan rejection, and mixed-rule rewrite | help and non-interactive token guard; no live mutation | Manual/live only because policy mutation is tailnet-global |

## Package capability matrix

| Package | Responsibility | Required test emphasis |
| --- | --- | --- |
| `cmd/serverpro` | Binary entrypoint wires CLI. | Build, `main_test.go` version path, root smoke. |
| `cmd/genbootstrapwrapper` | Prints the generated manual bootstrap wrapper from the pin manifest. | Exercised through `make gen-bootstrap-wrapper` and the bootstraptools shell suite. |
| `cmd/serverpro-e2e` | Build-tagged hermetic composition entrypoint. | Built only by `make test-full-chain-e2e`; requires a loopback fixture URL. |
| `internal/e2e` | Stateful all-provider compiled journeys and CI contract. | Concurrent isolated HOME journeys, strict JSON, production doctor/cleanup orchestration over local clients, completion-checkpoint failure, cleanup evidence, and secret-scrubbed failure artifacts. |
| `internal/cli` | Cobra commands, prompts, config/state orchestration, output, scoped flags. | Every command path, manifest-backed compiled evidence, strict JSON stability, non-interactive failures, dry-run no-write guarantees, prompt/progress routing to stderr, fixed progress phase/elapsed/attempt fields, secret redaction. |
| `internal/bootstraptools` | Embedded host-tool bootstrap script and target selection. | Supported Ubuntu/codename/architecture gate before mutation, complete package-floor and base-package manifests, candidate-floor rejection before install, locale-stable apt parsing, equal/newer/older/missing/removed-`config-files` package cases, all-target base-package convergence, canonical mise specification-to-manifest/doctor mapping, focused Git prerequisite sequencing and gh-only repair, executable configuration calls, shared readiness/final-verification probes including wrong npm, generated-script target/package apply-and-upgrade behavior, refreshed apt freshness check, exact Node/npm/Pi/uv/Rust/gh/rg/fd/ast-grep/sem/inspect/Herdr pins, release-shaped uv/ast-grep output, absent/present/failure legacy `sg` config migration, GitHub release checksums and architecture gates, inspect digest-before-execution, explicit backend and Rust-profile validation, forced same-version Node/npm, Rust, and checksum-tool repair with Pi restoration after Node replacement, component-scoped repair, malformed/missing/wrong-version probes, Rust default-component checks, script env, the bash `apt_update` lists-lock retry, budget exhaustion, and immediate non-lock failure with apt's status, positive-seconds validation for the wait settings, a single `DPkg::Lock::Timeout` call site, shell syntax, no `curl\|sh`, and executable exact-primary-key GPG allow/deny cases. Doctor Herdr check verifies the digest before any Herdr execution and names an older pinned install as an expected/got version mismatch read from the mise install path, executed for the other-version and same-version-tampered branches (container dogfood: stale, tampered, and out-of-tree binaries never ran). An expired-root harness (a `sudo` that fails as root) proves `run_as_target` and the doctor user-home drop use `runuser` with a reset environment, and one guard fails if `sudo -u`, `sudo -H`, or `sudo -i` appears, even split across backslash-continued lines, in any Go-rendered script that runs as root on a host: bootstrap install, wrapper, checks and package refresh, cloud-init user data, cloudflared install, repair and check, and the Tailscale updater and check. |
| `internal/cloudinit` | First-boot hardening user data. | Required admin/auth/hash inputs, defense-in-depth Ubuntu 24.04/codename/architecture check, candidate floors before cloud-init package install including removed-`config-files` state, complete post-install base-package floors, package-list refresh through the shared lists-lock helper with the shared dpkg-lock budget on upgrade and install, cleanup trap before fallible commands, SSH hardening, UFW, unattended upgrades (the parsed unattended-upgrades drop-in equals the doctor repair content), AppArmor, journald, pinned Tailscale version/checksums, no `curl\|sh`, executable amd64/arm64 success and checksum/architecture failure paths, no secret leaks in errors. |
| `internal/compute` | Full registry-facing provider facade, catalogs, diagnostics, registry. Consumer packages own narrower discovery/create contracts. | Registry validation/sorting, diagnostics semantics and wrapped-cause identity, managed-resource typed/legacy canonicalization and conflict rejection, provider account tokens excluded from all request JSON, full adapters composing through narrow consumer fakes, and the optional metadata user-data path and image accounts that each adapter declares. |
| `internal/config` | Defaults, paths, names, validation, YAML IO. | Strict supported-key decoding, byte-snapshot parsing, conditional locked publication, locked read-modify-write preservation, source appearance/removal/edit rejection, retired inert-key rejection, namespace/server validity, explicit create catalog requirements, typed Git access values, deploy repository scope rules and round trips, omitted lockdown defaults to enabled while explicit false is preserved, Tailscale mandatory defaults, ingress choices, private paths, schema stamping with legacy `store_console_password: false` migrated to stored, stamped opt-outs honored, unknown schema versions rejected. |
| `internal/credentials` | Server-scoped credential IO and validation. | 0700/0600 permissions, symlink rejection, shared local-artifact cleanup coordination, scoped incomplete import sets, missing-secret matrix per ingress, no global token storage, stored operator auth (sudo password, GitHub PAT, Tailscale auth key) round trips and redaction inclusion, auth-key acceptance, locked read-modify-write merges that keep a credential another run just rotated, refused mutations leave the file untouched, credential writer serialization. |
| `internal/doctor` | Local/provider/remote validation report. | Local tool checks, sshd checks without nested sudo (a guard fails if sudo returns to any sshd command, and a read runs against a sudo that fails as root), the sshd fix executed against a scratch `/etc/ssh` (no change with the service inactive or active, a change, a `mktemp` failure, an invalid candidate that leaves the live files untouched, a live `sshd -t` failure that restores a new or an existing drop-in, a failed write, a failed or still-invalid restore with its distinct exit status, a signal before any write, the drop-in written before the main config, a `Match`-scoped and a relative `Include`, and a lower-numbered drop-in that hides a value), no second run of an unsettled shared repair, the `sshd units` read and fix executed against a fake `systemctl` (failed, enabled but inactive, socket active with the service inactive, `start-limit-hit`, failed socket, both units masked with OpenSSH off), live re-reads after the sshd repair including the stale-baseline unit case and a single repair for several failing settings, the cloudflared repair wiring and its deadline for both the managed and the ingress-`none` unmanaged pair (one shared apt lock wait each for the lists and dpkg locks plus a transfer margin), one-call compute/mesh/explicit Cloudflare tunnel snapshot fakes, injected public-SSH probes, exact Git identity/signing reads and fixes with first-failure preservation, first-in-plan supported-platform authority, no sequential or batched fixes after platform failure, explicit batched read plans with conditional reads, strict baseline replay, no fixes after per-command output overflow, planned fix/recheck delegation including package-refresh authority, sequential fallback, captured package/tool failure output, terminal repair causes after noisy progress, credential redaction before 4 KiB head/tail truncation, UTF-8 boundaries, remote-only sudo retry, Tailscale node checked by recorded device ID with `tailscale_device_identity` coding for identity conflicts that blocks every remote command and sudo retry, bootstrap tools, Cloudflare connector, `gh token parity` verdicts sourced from GitHub (accepted, rejected, unanswered, absent) with no credential write on an unanswered or silent probe, deploy recheck outcomes, PAT-on-stdin probe header, strict curl stdin-config consumption, real-curl loopback Authorization delivery without token arguments, and the read-only drift probes executed under `sh` with fake host tools (UFW outgoing policy, every lockdown `ALLOW OUT` rule, and the blocked 853 connect, reboot flag and newer kernel (with the exact operator commands and no repair), unattended-upgrades effective values against a fake `apt-config` (set, unset, overridden), metadata user-data presence against a fake curl with no secret in the output, Tailscale operator with no prefs leak, Serve backends against `ss` listeners and Funnel, sysctl runtime and drop-in drift), the `cloudflared ingress` verdict with ingress `none` (a live connector fails, a dormant or absent one passes, an unclassifiable host fails), the provider-declared metadata path with a skip when absent, and the tailnet allow-all policy warning. The drift repair runs only on readable drift, re-reads after it, reports a repair that did not settle it, and skips a repair another check already ran; restricted egress declares the provisioning lockdown as its repair, `openssh disabled` declares the create step and is skipped when config enables OpenSSH, its repair runs without a tunnel and does not run when the tunnel's remote ingress cannot be read (`fix not run`), a repair's warning lines turn a fixed result into a warning, the unattended-upgrades check declares the drop-in rewrite, and privileged accounts, graded from a probe output with sudo's parsed rules (cvtsudoers JSON), runs the image account removal only when a declared account is in the findings and keeps a report-only finding left after it at its own severity. The probe's host side (groups, uid 0, image accounts, the parser call, and the path walk) is covered by the container runs under Provider image account dogfood, not by unit tests. `reboot survivability` declares the gated reboot install script as its repair only with `reboot.enabled: true`, and is report-only with the gate off. |
| `internal/filedescriptor` | File descriptor safety helper. | Descriptor limits and error cases. |
| `internal/hostplatform` | Controller, managed-host, architecture, and direct apt-package support baselines. | Exact support matrix, every direct package floor, package-group composition, package-name/apt-token/manifest rendering, immutable returned slices, and the shared apt lock wait: the rendered `apt_update` helper under dash and bash with a fake `apt-get` and `sleep` retries only the lists-lock error and then succeeds, gives up after the full budget with apt's status and error plus a clear message, fails at once on any other error, and forces `LC_ALL=C`. |
| `internal/importsync` | Discover/import recovery from provider labels through a read-only consumer contract. | Managed/unmanaged filtering, provider-recovered typed access-policy persistence, valid provider-only mesh intent, legacy disabled-mesh repair, omitted-token credential merge, duplicate labels, dry-run, preserving force refresh, malformed-existing-artifact and concurrent-config-drift rejection, same-tunnel provenance retention without ownership transfer, context-cancellable canonical workflow locking and matching-tailnet serialization, stable tailnet persistence, filesystem errors fail closed, injected config/credential/state/registry failures, marker-based retry, state-without-registry discovery, Tailscale/Cloudflare enrichment matchers (`enrich_test.go`). |
| `internal/ingress` | Generic ingress route model and Cloudflare Tunnel pending adapter. | Add/remove validation, pending status, no public route mutation claims. |
| `internal/lifecycle` | Provision sequence and state checkpoints through a create-only compute contract. | Tunnel install run under the shared apt-sized connector deadline, image account removal after the admin password step (only for declaring providers, never the admin or root, in safe step order, typed `remote_image_users` failure), the OpenSSH-off script as the last remote create script, after a remote ingress read that finds only the catch-all rule (after the lockdown and the gated reboot install, which runs only with `reboot.enabled: true`), the egress lockdown after the tools in create and as the last step of `server bootstrap` (skipped only when config defers it), a create with no reachable host failing closed under restricted egress, Narrow compute fakes, typed phase/resource failures, device bind bounded by the bootstrap key time (bounded local-clock fallback) then pinned to the recorded device ID on rerun, leftover device records ignored on fresh compute, provider recorded before the first provider call, checkpoint save-failure matrix, field-preserving concurrent ingress/status checkpoints, persisted tailnet identity plus missing/token-relative migration and conflict rejection, auth-key compensation, Tailscale policy/auth key cleanup, Cloudflare tunnel exact-name adoption/ambiguity/created-vs-adopted provenance/fresh rollback, compute create reconciliation, state stat errors, wait loops, remote bootstrap, omitted-lockdown convergence, shared Ed25519 fresh/rerun/public-key repair, Git deploy access, exact managed deploy-to-account cleanup, PAT stdin isolation, partial failure state. |
| `internal/mesh` | Provider-neutral mesh types, canonical device identity matching, and fail-closed device selection. | Short/FQDN/trailing-dot normalization, required-tag matching, stale same-name devices excluded by enrolment time, undated devices failing closed, ambiguity listing every candidate ID, recorded-device binding with missing/changed rejection and no name fallback, unknown SSH-rule field preservation through destination rewrites, and packet-policy allow-all detection (absent sections, empty acls, legacy users/ports, scoped and port-limited rules, and the literal allow-all acl and grant; the other spellings of everything are proven by the harness runs under Tailnet policy allow-all dogfood and Unit-test cases moved to harness runs). |
| `internal/network` | Network policy primitives. | Egress modes and allow-list behavior; the restricted lockdown carries its own fail-closed proof (UFW default, blocked connect, allowed connect). |
| `internal/ownership` | Provider ownership labels/tags. | Reversible encoding, cross-provider label equivalence, live ownership validation. |
| `internal/lifecycle` admin password | Post-boot admin password set shared by create and `doctor --fix`. | Plain `chpasswd` over protected stdin (no `--encrypted`, no precomputed hash), first-boot grant removal with sudoers rollback, set before any other remote step; cloud-init renders no password material. |
| `internal/rebootgate` | Gated reboot script, units, and install script rendered from the `reboot` config section. | `reboot.window` and `reboot.verify_timeout` rendered into the timer calendar, the script settings, and the verify unit's runtime limit; file modes; distinct digests per file; the install script and the inline preflight parse under `sh`; invalid windows and timeouts rejected. Host behaviour is covered by the gated reboot dogfood. |
| `internal/poll` | Shared cancellable polling wait policy. | Injected deterministic waits, cancellation, and timer fallback without wall-clock sleeps in orchestration tests. |
| `internal/privatefile` | Private atomic file writes and advisory coordination. | Shared/exclusive context-cancellable lock serialization, permissions, root-scoped parent opening, symlink rejection, atomic rename failure behavior, file and parent-directory sync. |
| `internal/provider/hetzner` | Hetzner compute adapter. | Catalog mapping, labels, firewall/access policy, create/status/power/delete, paginated server/firewall inventory, exact owned-policy recovery and checkpoint retry validation, foreign/broadened/attached policy rejection, action waits, not-found handling, bootstrap redaction. |
| `internal/provider/vultr` | Vultr compute adapter. | Declared `linuxuser` image account, catalog mapping, tag encoding, IPv4-only create, firewall group lifecycle, paginated firewall-rule inventory, checkpoint retry reconciliation that creates only missing required rules while rejecting foreign/broadened rules or attachments, status/power/delete/list, error identity and redaction. |
| `internal/provider/digitalocean` | DigitalOcean compute adapter. | Catalog mapping, one namespace/server-derived firewall selector, droplet ownership/custom tags, no direct firewall droplet-ID attachments, IPv4-only create, status/power/delete, all-resource delete preflight, guarded legacy broad-selector import/cleanup with zero-unrelated-match inventory, paginated droplet/firewall inventory, malformed pagination fail-closed behavior, exact owned-policy recovery and checkpoint retry validation, missing/ambiguous/foreign/broadened fail-closed behavior, error identity and redaction. |
| `internal/provider/tailscale` | Tailscale API adapter. | Auth key create/list/tracked-ID delete, devices, device waits that skip devices enrolled before the key and return ambiguity without polling, ACL policy read/validate/update, conservative global reconcile planning, retained tag-owner dependency protection, approved-plan refetch equality, no-change behavior, custom-shape retention, mixed-rule destination rewrite, exact `If-Match` propagation, missing-ETag mutation rejection, and a read-only HuJSON packet-policy read with API errors propagated. |
| `internal/provider/cloudflare` | Cloudflare API adapter. | Account validation, tunnel list/create/token/get/delete, remote ingress read (with and without a remote configuration, with empty private network and hostname route lists; configuration-wide defaults, private routes, and an empty or foreign answer are proven under Tunnel SSH route preflight dogfood), connector health polling, pagination. |
| `internal/provider/httpjson` | Shared HTTP JSON client. | Auth headers, JSON encode/decode, bounded body, status errors (method, path, and body in the text; ID and query masking and the UTF-8 cut are proven under Provider error evidence dogfood), redaction by caller. |
| `internal/provider/providerutil` | Shared provider mutation validation and secret-safe diagnostics. | Provider mismatch, token/bootstrap redaction, encoded bootstrap forms. |
| `internal/redact` | Secret redaction. | Token/password/hash/auth-key removal from strings/errors; redacted errors preserve sentinel, cancellation, and typed identity through `errors.Is`/`errors.As`. |
| `internal/releasecontract` | Test-only release workflow contract. | Parsed YAML dependency graph, exact complete validate/build/package/release step order and uniqueness, exact target/runner/GOOS/GOARCH build matrix and build environment, exact native build output argument, target-paired binary handoff and archive input, target-paired SBOM attestations, and unique prerelease classification before publication. |
| `internal/remote` | Tailscale SSH sudo runner. | Sudo password requirement, payload framing, context deadlines, fallback timeout, shell quoting, strict base64 batch framing, ordering, typed per-command/aggregate output limits, bounded transport capture, per-command failures, transport failures, and secret-free batch errors. |
| `internal/servername` | Server name normalization. | Default and invalid name cases. |
| `internal/shell` | Shell quoting. | POSIX-safe quoting matrix. |
| `internal/tailscaletools` | Shared Tailscale release manifest, remote version check, and live updater. | Exact version/digest pins, Ubuntu 24.04/codename/architecture gate before apt mutation, prerequisite candidate floors before install plus post-install floors, removed-`config-files` state rejection, client plus daemon checks, the bash `apt_update` lists-lock retry, budget exhaustion, and immediate non-lock failure, shared wait values in the script env, syntax, production `/bin/sh`/POSIX-mode delivery with bounded Bash re-exec, checksum extraction markers, TLS hybrid-default retention, and delayed restart that preserves the updating SSH command. |
| `internal/state` | Provider-neutral state/registry IO, workflow lock ownership, and shared local-artifact coordination. | Schema normalization, Cloudflare tunnel provenance ownership matrix, canonical/legacy registry union and conflict rejection, typed-only managed-resource writes with legacy-key migration/conflict rejection, per-server locked read-modify-write concurrency, cross-process registry updates, context cancellation/deadline behavior, partial-acquisition release, canonical namespace-before-server ordering, shared/exclusive namespace and tailnet locks, token-relative global policy exclusion, propagated save errors, missing-vs-stat-error checks, durable private writes. |
| `internal/testhttp` | Concurrency-safe HTTP fixture failure recorder. | Concurrent recording and outer-test-goroutine assertions; handlers record and return instead of calling fatal test methods. |
| `internal/tunnel` | Cloudflared install, diagnostic, and doctor repair scripts. | Ubuntu 24.04/codename/architecture gate before mutation, explicit noble apt source, candidate floor before install plus post-install minimum and service check, removed-`config-files` state rejection in install and diagnosis, the `pkg.cloudflare.com` origin pin written before apt in install and repair, candidate-origin rejection when another repository offers the version (real-compare floor including one step below it, the `***` installed marker, the dpkg status line before or after another source, an unrecognised source line, and replayed apt 2.8.3 output; see Cloudflared candidate-origin gate dogfood), exact-version install of the newest Cloudflare-only version, install-receipt match before an installed connector is kept (a missing receipt or one for another build forces a reinstall, including over a newer foreign version), package-list refresh through the shared lists-lock `apt_update` helper (executed in the upgrade script with one lock failure, then success), the shared `DPkg::Lock::Timeout` budget on every install, executable exact-primary-key acceptance, mismatch/additional-primary rejection before trusted install, and shell syntax. The ingress-`none` audit and repair run under dash against a fake dpkg, apt, systemd, and pgrep host: absent (including `config-files`), active, and dormant pass or repair; below-floor, missing-receipt, foreign-receipt, stopped-but-enabled, `half-installed` with or without a binary, a binary without a package, a stray binary on `PATH`, a static unit, a `dpkg-query` error, and a process outside the service fail; a dormant repair needs no unit or token, never enables or restarts the service, and fails when the reinstall enables it; an active repair needs the token; a changed apt source stops before apt; every successful repair is followed by a passing recheck. The host-state read behind the doctor `cloudflared ingress` check round-trips through its parser, the exact apt pin check fails a missing pin and a lowered priority, and the pin repair touches no apt, service, or token. Container dogfood covers check reasons and repair trust gates (see Cloudflared check and repair dogfood), installed-connector provenance (see Cloudflared install receipt dogfood), and connectors outside managed ingress (see Unmanaged cloudflared audit dogfood). |

## Feature/use-case matrix

| Use case | Unit | Integration | E2E/smoke | Dogfood |
| --- | --- | --- | --- | --- |
| Supported runtime and package baselines | host-platform matrix, provider image aliases, package groups, exact tool manifests, version probes | live-create catalog filtering/preflight, executable shell gates, candidate-before-install rejection, post-install floors, and doctor fix blocking reject unsupported state while accepting reviewed/newer versions | generated-wrapper equality, Ubuntu 24.04 image examples, non-live checks | Ubuntu 24.04 amd64/arm64 live dogfood when credentials and disposable hosts are explicitly approved |
| First dry-run for known catalog values | config validation, plan build | CLI create dry-run | no-token provider create previews for all providers | Read-only |
| First live create | cloud-init, lifecycle, providers, doctor | fake lifecycle with typed phase/resource checkpoint failures; no-compute-ID access-policy cleanup for every provider | hermetic compiled all-provider full chain plus checkpoint-failure recovery | Live optional create suite |
| Existing host bootstrap | target parser, generated script, Git intent validation | CLI bootstrap dry-run with state fixture; focused Git prerequisite/install/verification sequence; deploy/account persistence and transition cleanup | all target dry-runs | Live optional idempotent target; manual GitHub key/PAT registration |
| Status and inventory | state row builders, provider status | fake provider refresh | fixture list/status | Live status after create |
| Doctor and remediation | doctor check units, provider snapshot call counts, refreshed apt freshness, release-shaped uv/ast-grep output, exact Rust/Tailscale/curated developer-tool pins, inspect runtime digest, Rust default-profile components, batch parser | fake local/remote/provider clients; proactive metadata refresh, package/tool repair, forced same-version Node/npm, Rust, and checksum-tool repair with Pi restoration, production-shell Tailscale delivery, delayed Tailscale repair, batch/fix replay, overflow fail-closed behavior, and remote-only sudo retry | production-orchestrated full-chain doctor reports plus dry-run/token guards | Live doctor then `--fix` and repeat after create |
| Power operations | action validation | fake provider facade | dry-run start/stop/restart | Live optional, guarded |
| Delete and cleanup | typed managed-resource/migration/coalescing/conflict and tunnel-provenance units | all-provider typed access-policy checkpoints and preview, pre-lock registry/state-path binding, authority revalidation, external preflight, partial JSON with retained retry authority, guarded canonical artifact purge with registry-last failure handling, custom-path preservation, same-server serialization, and no global ACL mutation | production-orchestrated full-chain external and canonical local cleanup, compiled failure/retry journeys, plus dry-run fixtures | Live cleanup after create |
| Tailnet ACL reconciliation | exact-shape policy matching, retained owner-reference dependencies, and mixed-destination rewrite | explicit persisted identity, matching-tailnet state/live-device evidence, create/import lock coordination, fail-closed unresolved state, one-document stdout, unchanged-plan refetch/validate/ETag apply | help and no-token guard only | Manual explicit operation; live apply requires exact-plan approval |
| Lost-local-state recovery | ownership extraction, missing-vs-I/O-error classification | fake provider discover/import; all-provider typed access-policy recovery; Hetzner/DigitalOcean paginated policy lookup and missing/ambiguous rejection; bounded DigitalOcean legacy-firewall recovery with unrelated-match rejection; transactional stage failures and intent-preserving forced retry; forced refresh preserves operator intent, Tailscale policy evidence, ingress, and same-tunnel provenance while rejecting malformed existing config | compiled DigitalOcean legacy import→delete journey; token/argument guards | Live discover read-only; import only in isolated HOME |
| Optional ingress | route adapter and exact-name matcher units | Lifecycle retry adoption, ambiguity, created/adopted/imported provenance, fresh checkpoint rollback, and retained non-owned tunnel cleanup; CLI add/list/remove fixture | local pending-route flow | Cloudflare live only through guarded create |
| Provider catalogs | mapping units | fake HTTP/client catalog tests | token guards | Live read-only catalogs |
| Provider token validation | diagnostics units | fake HTTP/client doctor tests | token guards | Live provider doctor |
| Secret handling | redact/privatefile/account-JSON units | CLI credential tests; error identity preserved through redaction | no-token surface asserts no credential files from dry-runs | Live logs reviewed for redaction |
| Output contract | JSON marshal tests | CLI tests decode JSON | compiled no-token harness rejects malformed/noisy JSON before value assertions | `scripts/dogfood_validate.py` parses every zero-exit result and validates command-specific shape, status, action, and identity; artifacts retain stdout/stderr |
| Security invariants | validation units | lifecycle/doctor/provider tests | dry-run and guard cases | Live public SSH/doctor evidence |

## Live dogfood contract

`scripts/test-dogfood-live.sh` remains the single public live API orchestrator.
It sources `scripts/dogfood-live-lib.sh` first, which owns the shared helpers
and declares every shared run-context variable in `dogfood_context_init`. It
then sources `scripts/dogfood-live-readonly.sh` for provider reads,
`scripts/dogfood-live-create.sh` for explicitly approved paid lifecycle work,
`scripts/dogfood-live-identity.sh` for the tailnet device identity scenario, and
`scripts/dogfood-live-prompt.sh` for input prompts. Each flow file sources the
lib itself and depends on nothing else, so source order is free.
`scripts/dogfood_validate.py` owns importable output contracts and the
harness's JSON helper subcommands (state field reads, discover filtering, age
calculation, decoy key request and response, device counting, and the
credential writer), so the shell holds no inline Python. The orchestrator
uses an isolated `HOME`, never reuses operator state, and removes its temp home
unless `SERVERPRO_KEEP_HARNESS_TEMP=1` is set. Keep mode is the one exception:
`SERVERPRO_DOGFOOD_KEEP_SERVER=1` uses a dedicated persistent `0700` home
(`SERVERPRO_DOGFOOD_HOME`, default `~/.local/state/serverpro-dogfood/home`, never
the operator's own home) so one on-demand test server survives between runs
until the `delete` scenario removes it. A throwaway run refuses to start while
that home still tracks the same server, because both modes share provider
resource names. Guard rails: namespace and server
identifiers must match the CLI's `ValidID` grammar before any path is built,
`SERVERPRO_DOGFOOD_INGRESS` accepts only `none` or `cloudflare-tunnel` and fails
closed otherwise, tokens reach helper processes through the environment rather
than process argument lists, every zero-exit command must pass strict JSON plus
command-specific semantic validation, discovered candidates must prove the
requested provider and complete managed identity, and a failed delete is retried
once from the exit trap; when that fallback fails or returns invalid resource-identity
evidence, the harness keeps the created-resource markers, preserves the run
artifacts, and exits nonzero. SIGINT, SIGTERM, and SIGHUP route through the same
exit trap, so an interrupted throwaway run still deletes its server.

With a terminal attached, the harness asks for every missing input before any API
call (`scripts/dogfood-live-prompt.sh`): whether to run paid scenarios, the
provider token, the Tailscale token, the tailnet, the
sudo password (entered twice, at least 16 characters), and the Cloudflare token
and account ID for `cloudflare-tunnel` ingress. Secrets echo `*` per character,
never reach the screen, logs, or argv, and live only in harness variables.
Values already set in the environment are not asked for again; without a terminal,
or with `SERVERPRO_DOGFOOD_NO_PROMPT=1`, nothing is asked and the skip rules
apply. The default provider is DigitalOcean (`SERVERPRO_DOGFOOD_PROVIDER`).
`SERVERPRO_DOGFOOD_TEST_PROMPT_INPUT` exists only for the self-test's scripted
answers.

`SERVERPRO_DOGFOOD_SCENARIOS` selects a comma-separated subset of `create`,
`status`, `doctor`, `fix`, `bootstrap`, `power`, `import`, `identity`, and
`delete` (default `create,status,doctor,bootstrap,delete`). Scenarios always run
in that order; unknown names, and server scenarios without `create` outside keep
mode, exit 2 before any paid call. The order lives once, in
`DOGFOOD_SCENARIO_ORDER`; each name maps to a `scenario_<name>` function, and
every scenario except `create` and `delete` is skipped when no server is ready.

| Scenario | Proves |
|---|---|
| `create` | Create (or a resume in keep mode) ends with a passing doctor report. |
| `status`, `doctor`, `bootstrap` | Status row identity, passing doctor, idempotent `git` bootstrap. |
| `fix` | `server doctor --fix` passes, then plain doctor passes. |
| `power` | Stop reaches a settled off state, start reaches on and doctor passes, restart changes the kernel boot ID (a real reboot) and doctor passes again. |
| `import` | Import from provider labels into a separate empty `HOME` reports exactly one `imported` row, and doctor passes from the recovered artifacts. |
| `identity` | A short-lived decoy (a second userspace `tailscaled` on the test host, enrolled with an ephemeral, single-use, 10-minute tagged key) claims the server's recorded hostname and recorded Tailscale tag (read from state, not rebuilt); doctor and a create rerun still pass and the recorded node ID stays unchanged. The create rerun repeats the full tool bootstrap over the recorded node, so it proves remote commands reach that node beside the decoy and takes most of the scenario's several minutes. The decoy is logged out, stopped, and its key revoked when the scenario returns, and on interruption. |
| `delete` | Delete completes and clears the throwaway markers. |

Recovery waits poll every `SERVERPRO_DOGFOOD_POLL_INTERVAL` seconds (default 15)
up to `SERVERPRO_DOGFOOD_RECOVERY_TIMEOUT` (default 600), and every remote
`tailscale ssh` call, with its child processes, is stopped after
`SERVERPRO_DOGFOOD_SSH_TIMEOUT` seconds (default 60); all three accept only
positive integers. The keep-mode home is compared by physical path, so `.`,
`..`, doubled slashes, or a symlinked parent cannot alias the operator home or
a directory above it. Before a throwaway run, provider discovery in the dogfood
namespace must find no servers; after every run, including one ended by a
signal, it reports any still listed. A second signal during teardown is ignored
so the fallback delete always finishes. Every judged command's `PASS` or `FAIL` line ends with its wall time, so a slow
scenario can be traced to the step that spent it. The summary lists each scenario's result and
duration, the server's compute and node IDs and age, and warns when a kept server
reaches `SERVERPRO_DOGFOOD_MAX_AGE_HOURS` (default 24). The identity scenario
and power reboot check reach the host with the local `tailscale ssh`, so the
controller must be in the test server's tailnet; the decoy key request sends the
Tailscale token through curl's stdin config, and the sudo password and decoy key
reach the host only on stdin.

`scripts/test_dogfood_validate.py` table-tests every validator directly.
`scripts/test-dogfood-live-selftest.sh` then supplies valid fixtures for every
live command and proves empty, malformed, failing-status, wrong-action,
wrong-identity, invalid-catalog, and invalid-inventory outputs fail. It also
proves every provider command path, destructive opt-in guard, Cloudflare token
transport, and exact fallback cleanup payload/error retention. Fake `tailscale`
and `curl` binaries extend it to scenario selection, the full ordered run, keep
mode reuse and deletion, the kept-server age warning, the kept-state collision guard, leftover preflight, delete
after interruption and after a second signal during teardown, home aliases, an SSH hang cut off by the timeout, the sudo command shape, decoy failures with teardown, a recorded-node swap, an
import failed row hidden behind exit zero, prompted inputs with masking, backspace
editing, sudo retries, and required-value exhaustion, and secrets absent from every argv and
artifact. Both run through `make test-dogfood-live-selftest` as part of
`make check`, and `make lint-shell` (also in `make check`) lints every harness
shell file.

Read-only API dogfood runs when provider tokens are present:

```sh
SERVERPRO_DOGFOOD_HETZNER_TOKEN=... make test-dogfood-live
SERVERPRO_DOGFOOD_VULTR_TOKEN=... make test-dogfood-live
SERVERPRO_DOGFOOD_DIGITALOCEAN_TOKEN=... make test-dogfood-live
```

Set `SERVERPRO_REQUIRE_LIVE_DOGFOOD=1` to fail instead of skip when no live API
call ran.

Create/delete dogfood is destructive and paid-infrastructure creating. It must
use a supported Ubuntu 24.04 LTS amd64 or arm64 image and requires explicit
opt-in. The namespace defaults to the fixed `spdogfood`: create adds tailnet
policy tag owners and an SSH rule for the namespace tag, and delete never removes
tailnet-global policy, so a fixed namespace reuses one set of policy entries
instead of adding new ones on every run. Use a dedicated Tailscale API token for
dogfood runs.

```sh
SERVERPRO_DOGFOOD_CREATE=1 \
SERVERPRO_DOGFOOD_PROVIDER=digitalocean \
SERVERPRO_DOGFOOD_DIGITALOCEAN_TOKEN=... \
SERVERPRO_DOGFOOD_TAILSCALE_TOKEN=... \
SERVERPRO_DOGFOOD_TAILNET=example.ts.net \
SERVERPRO_DOGFOOD_LOCATION=fra1 \
SERVERPRO_DOGFOOD_SIZE=s-1vcpu-1gb-amd \
SERVERPRO_DOGFOOD_IMAGE=ubuntu-24-04-x64 \
SERVERPRO_DOGFOOD_SUDOPASS='long unique password here' \
make test-dogfood-live
```

On-demand kept test server: create once, run any scenarios later, delete when done
(each command also needs the create/delete variables above):

```sh
SERVERPRO_DOGFOOD_KEEP_SERVER=1 SERVERPRO_DOGFOOD_SCENARIOS=create make test-dogfood-live
SERVERPRO_DOGFOOD_KEEP_SERVER=1 SERVERPRO_DOGFOOD_SCENARIOS=doctor,fix,power,import,identity make test-dogfood-live
SERVERPRO_DOGFOOD_KEEP_SERVER=1 SERVERPRO_DOGFOOD_SCENARIOS=delete make test-dogfood-live
```

Optional Cloudflare Tunnel create path:

```sh
SERVERPRO_DOGFOOD_INGRESS=cloudflare-tunnel \
SERVERPRO_DOGFOOD_CLOUDFLARE_TOKEN=... \
SERVERPRO_DOGFOOD_CLOUDFLARE_ACCOUNT_ID=... \
# plus create/delete variables above
make test-dogfood-live
```

## Line coverage tracking

The capability matrix above tracks *behavioral* coverage. This section tracks
*line* coverage toward the 100% aim. `make cover` writes the merged
`coverage.out` profile and applies the same policy as CI. `make test-go-check`
combines that policy with the race detector so `make check` runs the Go suite
once.

| Measurement | Total statement coverage |
| --- | --- |
| Baseline (matrix-only) | 78.1% |
| Current supported-baseline upgrade | 85.3% |
| Enforced minimum | 81.8% |

Remaining uncovered code, by category (run `go tool cover -func` for the live
list). Each 0% function is either scheduled for a test, live-only, or accepted.
Remaining 0% functions: none.

| Function | Category | Plan |
| --- | --- | --- |

The enforced minimum aggregate coverage is 81.8%. Any lower total or any 0%
function fails `make cover`, `make test-go-check`, and therefore CI.

Rule: a 0% function must appear in this table with a category. "Dead code" means
delete it; "Live-only"/"Interactive" means the dogfood or manual layer owns it;
anything else is a real unit-test gap to close before the next capability lands.

## Expansion rules for new capabilities

1. Add the new command/package/use case to this matrix in the same change.
2. Add the smallest unit test before implementation.
3. Add fake-client integration coverage for every provider/service boundary.
4. Add or extend a compiled-binary e2e case in `scripts/test-cli-no-token-surface.sh` and map its passing label in `scripts/cli-surface-dispositions.tsv` when the behavior can run without live APIs. Extend `internal/e2e` when provider lifecycle composition changes.
5. Add a guarded live dogfood case in `scripts/test-dogfood-live.sh` when real provider behavior is part of the capability.
6. Update `README.md`, `USAGE.md`, `ARCHITECTURE.md`, `SECURITY.md`, or `DEVELOPMENT.md` only when the owning behavior or workflow changes.

No capability is complete until this file names its test evidence or its
explicitly accepted gap.
