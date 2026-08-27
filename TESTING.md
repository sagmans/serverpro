# Testing Matrix

Date: 2026-08-27
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
| Smoke | `make test-smoke` | Prove binary starts, help renders, and root doctor exits successfully (output discarded; JSON shape is covered by the consolidated Go gate and e2e layers). | No |
| Integration | `make test-integration` | Standalone focused rerun of CLI orchestration, provider HTTP adapters, lifecycle, import, doctor, state, and credentials with fakes or `httptest`; omitted from `make check` because the consolidated Go gate already includes it. | No |
| E2E | `make test-e2e` | Run the compiled CLI through isolated-home workflows, strict JSON parsing, inventory-derived help and parent coverage, disposition evidence, dry-runs, fixture state, and no-token safety checks. | No |
| Full-chain E2E | `make test-full-chain-e2e` | Build the test-only composition binary and run concurrent create→status→doctor→delete journeys against stateful local Hetzner, Vultr, and DigitalOcean APIs. Production doctor and cleanup orchestration consume injected local clients; fixed time, checkpoint recovery, strict JSON, cleanup evidence, and sanitized failure artifacts remain hermetic. | No |
| Release contract | `make test-release` | Run the focused release-contract Go package once plus shell assertions for the workflow DAG, exact step order/matrices, target-paired evidence, prerelease classification, toolchain pins, native smoke, and no-clobber invariants. `make check` gets the Go coverage from its consolidated suite and runs only the shell half separately. | No |
| Read-only dogfood | `make test-dogfood-readonly` | Dogfood the actual binary across every no-token command path and local state mutation path. | No |
| Live dogfood | `make test-dogfood-live` | Use real provider APIs for catalog, provider doctor, discover, and optional create→doctor→bootstrap→status→delete; every successful command must emit valid JSON with command-specific status, shape, and identity. | Yes |
| Live harness self-test | `make test-dogfood-live-selftest` | Unit-test every importable output contract, then prove malformed/invalid-success rejection, fallback-delete evidence, guards, secret transport, and cleanup retention through the shell orchestrator with a fake binary; no tokens or network. | No |

CI combines `make check` with a separate `make test-full-chain-e2e` job.
`make check` runs the primary non-live gates once; release workflow Go tests
stay in that consolidated invocation while the shell-only release contract runs
separately. The build-tagged full-chain journeys stay separate so Go
correctness, race, and aggregate coverage are not repeated inside the primary
job. Run both commands for complete non-live local parity. Live dogfood remains
opt-in so CI and local contributors do not create
paid infrastructure by accident.

## Command surface matrix

| Command | Capability | Unit/integration proof | Smoke/e2e proof | Dogfood proof |
| --- | --- | --- | --- | --- |
| `serverpro` | Root command, JSON default, version, timeout parsing, scoped global flags. | `internal/cli/root_test.go` | `scripts/test-cli-no-token-surface.sh` help/version/timeout cases | Read-only dogfood |
| `serverpro doctor` | Global provider/default ingress/Tailscale SSH sanity checks. | `internal/cli/global_doctor_command_test.go` tests through CLI | no-token `doctor` case | Read-only dogfood |
| `serverpro namespace` | Namespace parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro namespace create` | Create namespace config/state dirs with private permissions; dry-run preview; serialize against namespace server work. | `internal/cli/namespace_command_test.go` | namespace create + mode checks | Read-only dogfood |
| `serverpro namespace list` | Deterministic namespace inventory JSON. | `internal/cli/namespace_command_test.go` | empty and populated list cases | Read-only dogfood |
| `serverpro namespace status` | Namespace lookup and server counts. | `internal/cli/namespace_status_command_test.go` | status/missing namespace cases | Read-only dogfood |
| `serverpro namespace delete` | Preview, confirmation, exclusive namespace authority, post-lock registry plus parsed-state/missing-status revalidation, conditional stale cleanup, server cleanup delegation, local namespace removal. | `internal/cli/namespace_delete_command_test.go` | compiled dry-run plan plus no-write proof | Add live namespace cleanup only after server delete dogfood is stable |
| `serverpro server` | Server parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro server create` | Provider-agnostic create flow: explicit catalog selections, supported-image preflight, config completion, credentials, sudo password hashing, lifecycle, state checkpoints, stderr progress, doctor. | `internal/cli/create*_test.go`, `internal/cli/choices_test.go`, `internal/lifecycle/provision*_test.go`; live catalog filtering rejects unsupported OS/release/architecture entries, preflight rejects missing/unsupported selected images before mutation, config parsing uses the approved byte snapshot, conditional publication rejects appearance/removal/edit drift under coordinated file locking, namespace deletion is excluded, and token-relative policy work excludes explicit reconciliation | dry-run create plus hermetic compiled full-chain journeys for Hetzner, Vultr, and DigitalOcean; checkpoint-failure recovery | Live dogfood optional create→delete |
| `serverpro server bootstrap` | Re-run managed host tools on existing hosts for `all`, `git`, `docker`, `mise`, `node`, `pi`; emit stderr progress; the Git target converges Git/OpenSSH, target-user mise, and gh before persisted `none`, single-repo deploy-key, or PAT-required account-key setup. | `internal/cli/bootstrap*_test.go`, `internal/cli/git_identity_test.go`, `internal/bootstraptools/*_test.go`, `internal/lifecycle/git_deploy*_test.go`, `internal/lifecycle/git_identity*_test.go`; intent-before-mutation, required-PAT, exact doctor state, deploy-to-account cleanup, key rerun/public-key repair | dry-run all targets with fixture state | Live dogfood optional idempotent target after create; interactive GitHub registration remains manual |
| `serverpro server list` | Local registry/state listing with namespace/provider filters. | `internal/cli/server_read_command_test.go` | fixture list cases | Read-only dogfood |
| `serverpro server status` | State resolution, ambiguity handling, provider status refresh, IP update, JSON row. | `internal/cli/server_discovery_command_test.go`, `internal/cli/server_read_command_test.go` | no-token credential guard and `--all` cases | Live dogfood after create |
| `serverpro server doctor` | Local, provider, Tailscale, Cloudflare, SSH, blocking remote platform authority, remote hardening, sudo, exact tool pins, refreshed apt package freshness, and redaction checks; one snapshot per service, one read-only remote batch, remote-only sudo retry, optional package/tool/Tailscale fix path. | `internal/cli/server_doctor_command_test.go`, `internal/doctor/*_test.go`; unsupported platforms disable every sequential/batched fix, and `--fix` refreshes package metadata before trusting a cached-current result | dry-run/guard cases plus production-orchestrated compiled full-chain reports | Live dogfood after create |
| `serverpro server ssh` | Tailscale SSH target construction, admin user resolution, atomic config recovery update, dry-run command, live shell handoff. | `internal/cli/server_read_command_test.go`, `internal/config/io_test.go`, `internal/remote/*_test.go` including fake `tailscale` process | dry-run fixture case | Manual/live session only; do not automate interactive shell by default |
| `serverpro server discover` | Read provider inventory and filter serverpro ownership labels. | `internal/cli/server_discovery_command_test.go`, `internal/importsync/discover_test.go` | compiled help, provider validation, and scrubbed-token guard cases | Live read-only API dogfood |
| `serverpro server import` | Rebuild config, credentials, state, registry; valid provider-only mesh intent; partial credential completion; optional Tailscale and Cloudflare enrichment; preserving force refresh and service-token merge; dry-run/force/all/provider-id gates. | `internal/cli/server_import_command_test.go`, `internal/importsync/import_test.go`, `internal/importsync/enrich_test.go`, `internal/credentials/save_test.go` | compiled provider-only import → doctor dry-run → SSH recovery journey; help, argument/provider validation, scrubbed-token dry-run guard, and no-write proof | Live read-only discover first; import live only into isolated HOME when explicitly requested |
| `serverpro server start` | Ownership-checked provider power-on. | `internal/cli/server_power_delete_command_test.go`, provider adapter power tests | dry-run fixture case | Live dogfood optional after create |
| `serverpro server stop` | Ownership-checked graceful shutdown. | `internal/cli/server_power_delete_command_test.go`, provider adapter power tests | dry-run fixture case | Live dogfood optional; avoid stopping shared/manual fixtures |
| `serverpro server restart` | Ownership-checked reboot. | `internal/cli/server_power_delete_command_test.go`, provider adapter power tests | dry-run fixture case | Live dogfood optional after create |
| `serverpro server delete` | Destructive preview, confirmation, authority-drift rejection, ownership validation, compute delete, Tailscale cleanup, proven-created Cloudflare cleanup, retained adopted/imported/legacy tunnels, local state removal. | `internal/cli/server_power_delete_command_test.go`, `internal/cli/server_delete_cleanup*_test.go`, provider delete tests; pre-lock registry replacement cannot redirect or consume the approved state target | dry-run fixture cases plus production-orchestrated full-chain device cleanup evidence | Live dogfood cleanup path |
| `serverpro provider` | Provider parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro provider list` | Registered provider inventory. | `internal/cli/provider_catalog_command_test.go` | provider list case | Read-only dogfood |
| `serverpro provider status` | Provider capability JSON. | `internal/cli/provider_catalog_command_test.go` | status for Hetzner, Vultr, DigitalOcean | Read-only dogfood |
| `serverpro provider doctor` | Ephemeral token validation against provider APIs. | `internal/cli/provider_catalog_command_test.go`, provider adapter doctor tests with fake HTTP/clients | non-interactive token guard | Live read-only API dogfood |
| `serverpro catalog` | Catalog parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro catalog locations` | Provider location catalog via ephemeral token. | `internal/cli/provider_catalog_command_test.go`, provider catalog mapping tests | token guard for every provider | Live read-only API dogfood |
| `serverpro catalog sizes` | Provider size/plan catalog filtered by location. | Provider catalog mapping tests | token guard for every provider | Live read-only API dogfood |
| `serverpro catalog images` | Provider image catalog filtered by location where supported. | Provider catalog mapping tests | token guard for every provider | Live read-only API dogfood |
| `serverpro ingress` | Ingress parent help and unknown-child rejection. | `internal/cli/root_test.go` | parent help case | Read-only dogfood |
| `serverpro ingress add` | Add pending Cloudflare Tunnel route metadata without false public-route success claims. | `internal/cli/ingress_command_test.go`, `internal/ingress/ingress_test.go` | fixture add/duplicate/error cases | Read-only dogfood |
| `serverpro ingress list` | List local ingress state. | `internal/cli/ingress_command_test.go` | fixture list cases | Read-only dogfood |
| `serverpro ingress remove` | Remove local route metadata through adapter. | `internal/cli/ingress_command_test.go`, `internal/ingress/ingress_test.go` | fixture remove/error cases | Read-only dogfood |
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
| `internal/bootstraptools` | Embedded host-tool bootstrap script and target selection. | Supported Ubuntu/codename/architecture gate before mutation, complete package-floor and base-package manifests, candidate-floor rejection before install, locale-stable apt parsing, equal/newer/older/missing/removed-`config-files` package cases, all-target base-package convergence, canonical mise specification-to-manifest/doctor mapping, focused Git prerequisite sequencing and gh-only repair, executable configuration calls, shared readiness/final-verification probes including wrong npm, generated-script target/package apply-and-upgrade behavior, refreshed apt freshness check, exact Node/npm/Pi/uv/Rust/gh/rg/fd/ast-grep/sem/inspect/Herdr pins, release-shaped uv/ast-grep output, absent/present/failure legacy `sg` config migration, GitHub release checksums and architecture gates, inspect digest-before-execution, explicit backend and Rust-profile validation, forced same-version Rust/checksum-tool repair, component-scoped repair, malformed/missing/wrong-version probes, Rust default-component checks, script env, shell syntax, no `curl\|sh`, and executable exact-primary-key GPG allow/deny cases. |
| `internal/cloudinit` | First-boot hardening user data. | Required admin/auth/hash inputs, defense-in-depth Ubuntu 24.04/codename/architecture check, candidate floors before cloud-init package install including removed-`config-files` state, complete post-install base-package floors, cleanup trap before fallible commands, SSH hardening, UFW, unattended upgrades, AppArmor, journald, pinned Tailscale version/checksums, no `curl\|sh`, executable amd64/arm64 success and checksum/architecture failure paths, no secret leaks in errors. |
| `internal/compute` | Full registry-facing provider facade, catalogs, diagnostics, registry. Consumer packages own narrower discovery/create contracts. | Registry validation/sorting, diagnostics semantics and wrapped-cause identity, managed-resource typed/legacy canonicalization and conflict rejection, provider account tokens excluded from all request JSON, full adapters composing through narrow consumer fakes. |
| `internal/config` | Defaults, paths, names, validation, YAML IO. | Strict supported-key decoding, byte-snapshot parsing, conditional locked publication, locked read-modify-write preservation, source appearance/removal/edit rejection, retired inert-key rejection, namespace/server validity, explicit create catalog requirements, typed Git access values, deploy repository scope rules and round trips, omitted lockdown defaults to enabled while explicit false is preserved, Tailscale mandatory defaults, ingress choices, private paths. |
| `internal/credentials` | Server-scoped credential IO and validation. | 0700/0600 permissions, symlink rejection, scoped incomplete import sets, missing-secret matrix per ingress, no global token storage. |
| `internal/doctor` | Local/provider/remote validation report. | Local tool checks, one-call compute/mesh/explicit Cloudflare tunnel snapshot fakes, injected public-SSH probes, exact Git identity/signing reads and fixes with first-failure preservation, first-in-plan supported-platform authority, no sequential or batched fixes after platform failure, explicit batched read plans with conditional reads, strict baseline replay, no fixes after per-command output overflow, planned fix/recheck delegation including package-refresh authority, sequential fallback, remote-only sudo retry, Tailscale node, bootstrap tools, Cloudflare connector. |
| `internal/filedescriptor` | File descriptor safety helper. | Descriptor limits and error cases. |
| `internal/hostplatform` | Controller, managed-host, architecture, and direct apt-package support baselines. | Exact support matrix, every direct package floor, package-group composition, package-name/apt-token/manifest rendering, and immutable returned slices. |
| `internal/importsync` | Discover/import recovery from provider labels through a read-only consumer contract. | Managed/unmanaged filtering, provider-recovered typed access-policy persistence, valid provider-only mesh intent, legacy disabled-mesh repair, omitted-token credential merge, duplicate labels, dry-run, preserving force refresh, malformed-existing-artifact and concurrent-config-drift rejection, same-tunnel provenance retention without ownership transfer, context-cancellable canonical workflow locking and matching-tailnet serialization, stable tailnet persistence, filesystem errors fail closed, injected config/credential/state/registry failures, marker-based retry, state-without-registry discovery, Tailscale/Cloudflare enrichment matchers (`enrich_test.go`). |
| `internal/ingress` | Generic ingress route model and Cloudflare Tunnel pending adapter. | Add/remove validation, pending status, no public route mutation claims. |
| `internal/lifecycle` | Provision sequence and state checkpoints through a create-only compute contract. | Narrow compute fakes, typed phase/resource failures, checkpoint save-failure matrix, field-preserving concurrent ingress/status checkpoints, persisted tailnet identity plus missing/token-relative migration and conflict rejection, auth-key compensation, Tailscale policy/auth key cleanup, Cloudflare tunnel exact-name adoption/ambiguity/created-vs-adopted provenance/fresh rollback, compute create reconciliation, state stat errors, wait loops, remote bootstrap, omitted-lockdown convergence, shared Ed25519 fresh/rerun/public-key repair, Git deploy access, exact managed deploy-to-account cleanup, PAT stdin isolation, partial failure state. |
| `internal/mesh` | Provider-neutral mesh types and canonical device identity matching. | Short/FQDN/trailing-dot normalization, required-tag matching, and unknown SSH-rule field preservation through destination rewrites. |
| `internal/network` | Network policy primitives. | Egress modes and allow-list behavior. |
| `internal/ownership` | Provider ownership labels/tags. | Reversible encoding, cross-provider label equivalence, live ownership validation. |
| `internal/passwordhash` | Remote admin password hashing. | SHA-512 validity, weak/invalid hash rejection, no plaintext persistence. |
| `internal/poll` | Shared cancellable polling wait policy. | Injected deterministic waits, cancellation, and timer fallback without wall-clock sleeps in orchestration tests. |
| `internal/privatefile` | Private atomic file writes and advisory coordination. | Exclusive lock serialization, permissions, parent creation, symlink rejection, atomic rename failure behavior, file and parent-directory sync. |
| `internal/provider/hetzner` | Hetzner compute adapter. | Catalog mapping, labels, firewall/access policy, create/status/power/delete, paginated server/firewall inventory, exact owned-policy recovery and checkpoint retry validation, foreign/broadened/attached policy rejection, action waits, not-found handling, bootstrap redaction. |
| `internal/provider/vultr` | Vultr compute adapter. | Catalog mapping, tag encoding, IPv4-only create, firewall group lifecycle, paginated firewall-rule inventory, checkpoint retry reconciliation that creates only missing required rules while rejecting foreign/broadened rules or attachments, status/power/delete/list, error identity and redaction. |
| `internal/provider/digitalocean` | DigitalOcean compute adapter. | Catalog mapping, one namespace/server-derived firewall selector, droplet ownership/custom tags, no direct firewall droplet-ID attachments, IPv4-only create, status/power/delete, all-resource delete preflight, guarded legacy broad-selector import/cleanup with zero-unrelated-match inventory, paginated droplet/firewall inventory, malformed pagination fail-closed behavior, exact owned-policy recovery and checkpoint retry validation, missing/ambiguous/foreign/broadened fail-closed behavior, error identity and redaction. |
| `internal/provider/tailscale` | Tailscale API adapter. | Auth key create/list/tracked-ID delete, devices, ACL policy read/validate/update, conservative global reconcile planning, retained tag-owner dependency protection, approved-plan refetch equality, no-change behavior, custom-shape retention, mixed-rule destination rewrite, exact `If-Match` propagation, and missing-ETag mutation rejection. |
| `internal/provider/cloudflare` | Cloudflare API adapter. | Account validation, tunnel list/create/token/get/delete, connector health polling, pagination. |
| `internal/provider/httpjson` | Shared HTTP JSON client. | Auth headers, JSON encode/decode, bounded body, status errors, redaction by caller. |
| `internal/provider/providerutil` | Shared provider mutation validation and secret-safe diagnostics. | Provider mismatch, token/bootstrap redaction, encoded bootstrap forms. |
| `internal/redact` | Secret redaction. | Token/password/hash/auth-key removal from strings/errors; redacted errors preserve sentinel, cancellation, and typed identity through `errors.Is`/`errors.As`. |
| `internal/releasecontract` | Test-only release workflow contract. | Parsed YAML dependency graph, exact complete validate/build/package/release step order and uniqueness, exact target/runner/GOOS/GOARCH build matrix and build environment, exact native build output argument, target-paired binary handoff and archive input, target-paired SBOM attestations, and unique prerelease classification before publication. |
| `internal/remote` | Tailscale SSH sudo runner. | Sudo password requirement, payload framing, context deadlines, fallback timeout, shell quoting, strict base64 batch framing, ordering, typed per-command/aggregate output limits, bounded transport capture, per-command failures, transport failures, and secret-free batch errors. |
| `internal/servername` | Server name normalization. | Default and invalid name cases. |
| `internal/shell` | Shell quoting. | POSIX-safe quoting matrix. |
| `internal/tailscaletools` | Shared Tailscale release manifest, remote version check, and live updater. | Exact version/digest pins, Ubuntu 24.04/codename/architecture gate before apt mutation, prerequisite candidate floors before install plus post-install floors, removed-`config-files` state rejection, client plus daemon checks, syntax, production `/bin/sh`/POSIX-mode delivery with bounded Bash re-exec, checksum extraction markers, TLS hybrid-default retention, and delayed restart that preserves the updating SSH command. |
| `internal/state` | Provider-neutral state/registry IO and workflow lock ownership. | Schema normalization, Cloudflare tunnel provenance ownership matrix, canonical/legacy registry union and conflict rejection, typed-only managed-resource writes with legacy-key migration/conflict rejection, per-server locked read-modify-write concurrency, cross-process registry updates, context cancellation/deadline behavior, partial-acquisition release, canonical namespace-before-server ordering, shared/exclusive namespace and tailnet locks, token-relative global policy exclusion, propagated save errors, missing-vs-stat-error checks, durable private writes. |
| `internal/testhttp` | Concurrency-safe HTTP fixture failure recorder. | Concurrent recording and outer-test-goroutine assertions; handlers record and return instead of calling fatal test methods. |
| `internal/tunnel` | Cloudflared install and diagnostic scripts. | Ubuntu 24.04/codename/architecture gate before mutation, explicit noble apt source, candidate floor before install plus post-install minimum and service check, removed-`config-files` state rejection in install and diagnosis, executable exact-primary-key acceptance, mismatch/additional-primary rejection before trusted install, and shell syntax. |

## Feature/use-case matrix

| Use case | Unit | Integration | E2E/smoke | Dogfood |
| --- | --- | --- | --- | --- |
| Supported runtime and package baselines | host-platform matrix, provider image aliases, package groups, exact tool manifests, version probes | live-create catalog filtering/preflight, executable shell gates, candidate-before-install rejection, post-install floors, and doctor fix blocking reject unsupported state while accepting reviewed/newer versions | generated-wrapper equality, Ubuntu 24.04 image examples, non-live checks | Ubuntu 24.04 amd64/arm64 live dogfood when credentials and disposable hosts are explicitly approved |
| First dry-run for known catalog values | config validation, plan build | CLI create dry-run | no-token provider create previews for all providers | Read-only |
| First live create | cloud-init, lifecycle, providers, doctor | fake lifecycle with typed phase/resource checkpoint failures; no-compute-ID access-policy cleanup for every provider | hermetic compiled all-provider full chain plus checkpoint-failure recovery | Live optional create suite |
| Existing host bootstrap | target parser, generated script, Git intent validation | CLI bootstrap dry-run with state fixture; focused Git prerequisite/install/verification sequence; deploy/account persistence and transition cleanup | all target dry-runs | Live optional idempotent target; manual GitHub key/PAT registration |
| Status and inventory | state row builders, provider status | fake provider refresh | fixture list/status | Live status after create |
| Doctor and remediation | doctor check units, provider snapshot call counts, refreshed apt freshness, release-shaped uv/ast-grep output, exact Rust/Tailscale/curated developer-tool pins, inspect runtime digest, Rust default-profile components, batch parser | fake local/remote/provider clients; proactive metadata refresh, package/tool repair, forced same-version Rust/checksum-tool repair, production-shell Tailscale delivery, delayed Tailscale repair, batch/fix replay, overflow fail-closed behavior, and remote-only sudo retry | production-orchestrated full-chain doctor reports plus dry-run/token guards | Live doctor then `--fix` and repeat after create |
| Power operations | action validation | fake provider facade | dry-run start/stop/restart | Live optional, guarded |
| Delete and cleanup | typed managed-resource/migration/coalescing/conflict and tunnel-provenance units | all-provider typed access-policy checkpoints and preview, pre-lock registry/state-path binding, state/registry/config/credential authority revalidation, DigitalOcean all-resource preflight plus bounded legacy-selector recovery, legacy retry compatibility, same-server workflow serialization, fake provider and server-scoped cleanup; adopted/imported/legacy tunnels retained; no global ACL mutation | production-orchestrated full-chain device cleanup plus dry-run fixtures | Live cleanup after create |
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
It sources `scripts/dogfood-live-readonly.sh` for provider reads and
`scripts/dogfood-live-create.sh` for explicitly approved paid lifecycle work;
`scripts/dogfood_validate.py` owns importable output contracts. The orchestrator
uses an isolated `HOME`, never reuses operator state, and removes its temp home
unless `SERVERPRO_KEEP_HARNESS_TEMP=1` is set. Guard rails: namespace and server
identifiers must match the CLI's `ValidID` grammar before any path is built,
`SERVERPRO_DOGFOOD_INGRESS` accepts only `none` or `cloudflare-tunnel` and fails
closed otherwise, tokens reach helper processes through the environment rather
than process argument lists, every zero-exit command must pass strict JSON plus
command-specific semantic validation, discovered candidates must prove the
requested provider and complete managed identity, and a failed delete is retried
once from the exit trap; when that fallback fails or returns invalid resource-identity
evidence, the harness keeps the created-resource markers, preserves the run
artifacts, and exits nonzero.

`scripts/test_dogfood_validate.py` table-tests every validator directly.
`scripts/test-dogfood-live-selftest.sh` then supplies valid fixtures for every
live command and proves empty, malformed, failing-status, wrong-action,
wrong-identity, invalid-catalog, and invalid-inventory outputs fail. It also
proves every provider command path, destructive opt-in guard, Cloudflare token
transport, and exact fallback cleanup payload/error retention. Both run through
`make test-dogfood-live-selftest` as part of `make check`.

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
opt-in:

```sh
SERVERPRO_DOGFOOD_CREATE=1 \
SERVERPRO_DOGFOOD_CONFIRM=serverpro-live-dogfood \
SERVERPRO_DOGFOOD_PROVIDER=hetzner \
SERVERPRO_DOGFOOD_HETZNER_TOKEN=... \
SERVERPRO_DOGFOOD_TAILSCALE_TOKEN=... \
SERVERPRO_DOGFOOD_TAILNET=example.ts.net \
SERVERPRO_DOGFOOD_LOCATION=fsn1 \
SERVERPRO_DOGFOOD_SIZE=cx23 \
SERVERPRO_DOGFOOD_IMAGE=ubuntu-24.04 \
SERVERPRO_DOGFOOD_SUDOPASS='long unique password here' \
make test-dogfood-live
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
