# Development

DEVELOPMENT.md is contributor workflow. User docs live in `README.md`,
`INSTALLATION.md`, `USAGE.md`, and `SECURITY.md`.

## Toolchain

- Go 1.26.5 is the supported development and CI toolchain.
- `mise.toml` pins the local Go version.
- `make check` is the local and CI gate.
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
make test
make check
go test ./internal/cli -run TestName
make dogfood-no-token
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
- Generic provider behavior belongs behind `internal/compute`.
- State persistence belongs in `internal/state`.
- Remote execution belongs behind `internal/remote`.
- Provider adapters must not leak into generic packages except registry wiring.

## Code standards

- Use `gofmt`.
- Return errors outside program entrypoint; do not exit from libraries.
- Keep JSON stdout deterministic and secret-free.
- Route prompts and confirmations to stderr.
- Redact tokens, sudo passwords, hashes, auth keys, and bootstrap data.
- Save state checkpoints after provider resources are created.
- Destroy only state-known, ownership-validated resources.
- Prefer fake clients over live provider calls in tests.

## Security invariants

- Tailscale SSH admin path.
- Public SSH disabled.
- Public app ingress defaults to `none`.
- Remote root actions require admin sudo password.
- No unrestricted `NOPASSWD:ALL` managed state.
- Tokens never appear in output, logs, errors, state, docs, or tests.
- Destructive operations require confirmation and ownership validation.

## Release

Tag only a reviewed commit as `vX.Y.Z`; never move or reuse a release tag. Run
`mise exec -- make check` before tagging. The tag workflow reruns that gate with
Go 1.26.5 against the tagged commit, then builds Linux and macOS archives for
`amd64` and `arm64`. Windows builds are out of scope.

Each archive contains only the binary, project `LICENSE`, and
`THIRD_PARTY_NOTICES`. Packaging verifies the allowlist, `-trimpath` metadata,
version output on the host target, private-path/URL/credential patterns, and
SHA-256 digests. `RELEASE_MANIFEST` binds all four archive digests to the tag's
source commit and Go version. A complete release has six assets: four archives,
`SHA256SUMS`, and `RELEASE_MANIFEST`.

Publication is fail-closed: an existing release causes the workflow to fail
instead of replacing assets. Before announcing a release, release owner must
confirm the tag workflow is green, six assets exist, every checksum verifies,
the manifest names the tagged commit, and a host-target binary reports the tag.
Any missing asset, checksum mismatch, unexpected archive member, scan finding,
or execution failure stops rollout; zero exceptions are allowed.

Last reversible point is before tag push. Published tags and clean assets remain
immutable. Corrections use a new patch tag; release owner marks affected release
withdrawn and publishes forward-fix guidance. If an asset exposes secrets or
legally prohibited material, security or legal incident authority decides
whether removal outweighs evidence preservation. Project emits no usage
telemetry, so workflow status, artifact verification, and security/bug reports
are rollout health signals.

## Documentation rules

- README: purpose, safety, quick try, links.
- INSTALLATION: install and first run.
- USAGE: operator behavior and command surface.
- SECURITY: reporting, threat model, recovery boundaries.
- ARCHITECTURE: stable package boundaries and design decisions.
- AGENTS: document index only.
- Avoid duplicating code-owned details in docs.
