# Tailscale setup

serverpro uses Tailscale SSH as the normal administration path.

## Before using serverpro

1. Create or select a tailnet.
2. Create a Tailscale API token that can list member users and devices, manage
   auth keys, and read, validate, and update policy. Scoped credentials need
   `users:read` in addition to scopes for those other operations.
3. Make sure tag ownership and SSH policy can be updated for serverpro-managed
   tags.
4. Install the local `tailscale` CLI for `serverpro server ssh` and local checks.

serverpro rejects direct user-supplied auth keys and creates short-lived tagged
keys during provisioning.

## Official docs

- Tailscale API: <https://tailscale.com/docs/reference/tailscale-api>
- Credential scopes: <https://tailscale.com/docs/reference/trust-credentials>
- Tailscale SSH: <https://tailscale.com/kb/1193/tailscale-ssh>
- Auth keys: <https://tailscale.com/kb/1085/auth-keys>
- Tags: <https://tailscale.com/kb/1068/tags>
- ACL syntax: <https://tailscale.com/kb/1337/acl-syntax>
- Hetzner Tailscale install guide: <https://tailscale.com/docs/install/cloud/hetzner>
