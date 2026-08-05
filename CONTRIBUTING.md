# Contributing

Thanks for helping improve serverpro.

## Before work

- Read `README.md`, `USAGE.md`, `DEVELOPMENT.md`, `ARCHITECTURE.md`, and
  `SECURITY.md`.
- Open or link a GitHub issue for non-trivial changes.
- Use a small branch and keep changes focused.
- Do not include secrets, hostnames, public IPs, provider IDs, local paths, or
  live account values in issues, tests, docs, commits, or PRs.

## Issues and tasks

GitHub Issues is the public task tracker for serverpro. Use issues for bugs,
small feature proposals, follow-up tasks, and scoped implementation work. Keep
each issue narrow enough for one focused pull request.

Do not file security vulnerabilities as public issues. Follow `SECURITY.md`
instead.

## Development

Follow the end-to-end workflow in `DEVELOPMENT.md`.

```sh
make test
make check
```

Add tests next to changed code. Prefer fake providers over live infrastructure.
Use `--dry-run` for provisioning flows while developing. If live infrastructure
is unavoidable, explain the scope, provider resources, and cleanup proof in the
PR.

## AI policy

AI tools are allowed and encouraged, but contributors own the work. Review every
AI-assisted change with human eyes. Code is yours, not the model's. Use AI to
improve the process, not to replace judgment.

PRs should provide clean, high-quality code; not merely working code. Dogfood
and manually test user-facing behavior when practical. Explain motivation,
decisions, safety impact, and proof in the PR.

## Pull requests

PRs should include:

- purpose and scope
- linked GitHub issue when available
- tests/checks run
- security impact for live-infra changes
- docs updated when user behavior changes
- rollback or cleanup notes for provider-resource changes

Security vulnerabilities must follow `SECURITY.md`, not public issue comments.
