# AGENTS.md

AGENTS.md is the repository document index for coding agents. Keep only
navigation here. Put substantive content in the owning document. Update this
index when documents change.

## Document maintenance

Tracked documents stay in sync with the current branch state: when a change
alters behavior, scope, versions, file layout, terminology, or external
sources, update the owning document in the same change rather than deferring
to a later pass or a separate ticket.

- Behavior, package layout, terminology, or runtime flow → `ARCHITECTURE.md`
- Install steps, requirements, or managed tools → `INSTALLATION.md`
- Commands, targets, flags, or operator flow → `USAGE.md`
- Security controls, supply-chain, or access model → `SECURITY.md`
- External URLs, key fingerprints, or upstream version pins → `WEB_SOURCES.md`
- Contributor workflow → `DEVELOPMENT.md`
- Test approach, quality gates, or capability matrix → `TESTING.md`
- Purpose, scope, or top-level navigation → `README.md`
- New or removed documents → this index (`AGENTS.md`)

If a change touches a topic a document does not yet cover, add the coverage
rather than leaving a gap. A doc that drifts from code is worse than no doc.

## Document Index

- `AGENTS.md` — agent document index. (this file)
- `README.md` — human overview and top-level navigation.
- `ARCHITECTURE.md` — architecture reference.
- `INSTALLATION.md` — install and setup reference.
- `USAGE.md` — user and operator reference.
- `DEVELOPMENT.md` — contributor workflow reference.
- `TESTING.md` — test matrix, quality gates, and dogfood reference.
- `CONTRIBUTING.md` — public contribution guidance.
- `.github/pull_request_template.md` — pull request author checklist.
- `SECURITY.md` — security reference.
- `CODE_OF_CONDUCT.md` — public collaboration rules.
- `WEB_SOURCES.md` — external source reference.
- `docs/hetzner-account-setup.md` — Hetzner setup reference.
- `docs/vultr-account-setup.md` — Vultr setup reference.
- `docs/digitalocean-account-setup.md` — DigitalOcean setup reference.
- `docs/tailscale-setup.md` — Tailscale setup reference.
- `docs/cloudflare-tunnel-setup.md` — Cloudflare Tunnel setup reference.
- `LICENSE` — license terms.
