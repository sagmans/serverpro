# serverpro

[![CI](https://github.com/sagmans/serverpro/actions/workflows/ci.yml/badge.svg)](https://github.com/sagmans/serverpro/actions/workflows/ci.yml)

Small, security-focused server provisioner.

serverpro creates hardened Ubuntu servers on Hetzner, Vultr, or DigitalOcean;
keeps provider-neutral local state; uses Tailscale SSH for administration; and
keeps public app ingress off by default. Cloudflare tunnel lifecycle can create
or adopt an account tunnel and records pending local route metadata; it does
not publish publicly routable routes yet.

serverpro also converges the host toolchain — git, Docker, per-user mise, Node,
Pi, and Herdr — through an integrity-verified bootstrap script over Tailscale
SSH (see `SECURITY.md`). Herdr is version- and digest-pinned, and its Pi
integration is installed for authoritative agent lifecycle reporting.

serverpro prepares hosts and safe access paths. It does not deploy apps, manage
DNS records, run databases, or store application secrets.

## Status

Early public release. Interfaces and state shape can change before `v1.0.0`.
Use dry-runs when you already know provider catalog values, read `SECURITY.md`
before live use, and use at your own risk; the MIT License provides this
software without warranty.

Bugs and feature work are tracked in GitHub Issues. Security reports must follow
`SECURITY.md`, not public issue comments.

## Install

Install latest:

```sh
go install github.com/sagmans/serverpro/cmd/serverpro@latest
export PATH="$(go env GOPATH)/bin:$PATH"
serverpro --help
```

Install a specific release (recommended):

```sh
go install github.com/sagmans/serverpro/cmd/serverpro@vX.Y.Z
```

If `GOBIN` is set, add that directory to `PATH` instead. Requires Go 1.26.6+.

## Provider setup

Create provider accounts and tokens before live use:

- [Hetzner setup](docs/hetzner-account-setup.md)
- [Vultr setup](docs/vultr-account-setup.md)
- [DigitalOcean setup](docs/digitalocean-account-setup.md)
- [Tailscale setup](docs/tailscale-setup.md)
- [Cloudflare Tunnel setup](docs/cloudflare-tunnel-setup.md) optional

## Quick try

Create a namespace, then create a server inside it. The server command prompts
for provider catalog choices such as location, size, and image.

1. Create a namespace:

   ```sh
   serverpro namespace create mynamespace
   ```

2. Start interactive server creation:

   ```sh
   serverpro server create webapp -n mynamespace -p hetzner
   ```

3. Follow the prompts for compute shape, Tailscale access, optional Cloudflare
   Tunnel metadata, confirmation, and the remote admin sudo password.

## Defaults

- Tailscale SSH admin path required.
- Public SSH disabled.
- Public app ingress defaults to `none`.
- Host tools install through a checksum- and GPG-verified bootstrap script, not
  `curl|sh`.
- Server credentials stored per server with private file permissions.
- Remote admin sudo password is runtime-only.
- Destructive operations require known state, provider ownership metadata, and
  confirmation.

## Docs

- [Installation](INSTALLATION.md)
- [Usage](USAGE.md)
- [Security](SECURITY.md)
- [Architecture](ARCHITECTURE.md)
- [Development](DEVELOPMENT.md)
- [Release procedure](RELEASE.md)
- [Testing Matrix](TESTING.md)
- [Contributing](CONTRIBUTING.md)
- [License](LICENSE)
