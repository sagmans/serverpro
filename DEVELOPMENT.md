# Development

DEVELOPMENT.md is contributor workflow. User docs live in `README.md`,
`INSTALLATION.md`, `USAGE.md`, and `SECURITY.md`.

## Toolchain

- Go 1.26.6 is the supported development, CI, and release toolchain.
- `mise.toml` owns the exact Go version pin.
- `make check` is the primary local and CI gate;
  `make test-full-chain-e2e` is the separate required full-chain gate.
- Tool installers write under `.bin/`; caches stay under `.cache/`.

## Workflow

1. Inspect git state before changes.
2. Link non-trivial work to a GitHub issue.
3. Read the owning doc before behavior changes:
   - `ARCHITECTURE.md` for package boundaries.
   - `USAGE.md` for operator behavior.
   - `SECURITY.md` for security invariants.
4. Make one small coherent change.
5. Add/update tests next to changed code.
6. Run focused tests, then broader checks.
7. Update only docs that users or maintainers need.

## Commands

```sh
make fmt
make test-unit
make test-go-check
make test-smoke
make test-integration
make test-e2e
make test-full-chain-e2e
make test-release
make test-dogfood-readonly
make test-dogfood-live-selftest
make check
go test ./internal/cli -run TestName
make dogfood-no-token
```

`make check` uses `make test-go-check` to execute the default Go suite once with
race and one merged coverage profile. Its release shell contract does not rerun
the Go release package; `make test-release` remains the focused target that
runs that package once plus the shell assertions. Standalone unit, integration,
race, and cover targets support focused development. Build-tagged full-chain
journeys run through `make test-full-chain-e2e` in a distinct CI job. The gate
rejects aggregate coverage below 81.8% and any 0%-covered function.

`TESTING.md` owns the full capability matrix. Update it when adding or removing
commands, packages, providers, ingress modes, lifecycle steps, or dogfood paths.
Live API dogfood is opt-in:

```sh
SERVERPRO_DOGFOOD_HETZNER_TOKEN='...' make test-dogfood-live
```

Use dry-runs for live-infra flows while developing:

```sh
serverpro server create web -n mynamespace -p hetzner \
  --location fsn1 --size cx23 --image ubuntu-24.04 \
  --dry-run --non-interactive
```

## Package boundaries

- `cmd/serverpro` depends only on `internal/cli`.
- CLI orchestration belongs in `internal/cli`.
- Provider APIs belong in `internal/provider/<name>`.
- Full provider registration belongs in `internal/compute`; consumers define
  the smallest read or mutation interface they need.
- State persistence and cross-workflow lock ordering belong in `internal/state`.
- Remote execution belongs behind `internal/remote`.
- Provider adapters must not leak outside explicit CLI composition roots.

## Code standards

- Use `gofmt`.
- Return errors outside program entrypoint; do not exit from libraries.
- Keep JSON stdout deterministic and secret-free.
- Route prompts and confirmations to stderr.
- Redact tokens, sudo passwords, hashes, auth keys, and bootstrap data.
- Save state checkpoints after provider resources are created; validate every
  checkpointed access policy against live ownership and scope before retry.
- Pass caller contexts into operation locks; never duplicate namespace/server
  lock ordering outside `internal/state`.
- Destroy only state-known, ownership-validated resources.
- Prefer fake clients over live provider calls in tests.

## Security invariants

- Tailscale SSH admin path.
- Public SSH disabled.
- Public app ingress defaults to `none`.
- Remote root actions require admin sudo password.
- No unrestricted `NOPASSWD:ALL` managed state.
- Tokens never appear in output, logs, errors, state, docs, or tests.
- Destructive operations require confirmation, complete local authority, and
  live ownership validation.

## Release

Follow `RELEASE.md` for the signed-tag, GitHub-release, verification, and
failure procedure. It requires `make check` and `make test-full-chain-e2e`, and
excludes manual asset publication and infrastructure deployment.

## Documentation rules

- README: purpose, safety, quick try, links.
- INSTALLATION: install and first run.
- USAGE: operator behavior and command surface.
- SECURITY: reporting, threat model, recovery boundaries.
- ARCHITECTURE: stable package boundaries and design decisions.
- AGENTS: document index only.
- Avoid duplicating code-owned details in docs.
