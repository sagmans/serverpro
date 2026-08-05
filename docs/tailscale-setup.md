# Tailscale setup

serverpro uses Tailscale SSH as the normal administration path.

## Before using serverpro

1. Create or select a tailnet.
2. Create a Tailscale API token that can manage devices, auth keys, and policy.
3. Make sure tag ownership and SSH policy can be updated for serverpro-managed
   tags.
4. Install the local `tailscale` CLI for `serverpro server ssh` and local checks.

serverpro rejects direct user-supplied auth keys and creates short-lived tagged
keys during provisioning.

## Tailnet DNS configuration

Required tailnet setup for managed servers (admin console → DNS):

1. Enable MagicDNS (default for tailnets created after 2022-10).
2. Add at least two global nameservers. Recommended: Quad9 (`9.9.9.9`,
   DNSSEC-validating, malware-filtering) and Cloudflare (`1.1.1.1`). Tailscale
   transparently upgrades public-resolver upstream queries to DNS-over-HTTPS,
   so lookups leave the host encrypted on port 443 — which the restricted
   egress lockdown already permits.
3. Enable **Override DNS servers**. Without it, the control plane does not
   distribute global nameservers to nodes at all; quad100 then depends on
   reading the host's system DNS as fallback, which has failed silently in the
   wild (tailscaled 1.98.10) and kills all public name resolution.

### Resulting behavior on servers

- `--accept-dns` stays at its default (`true`): tailscaled manages
  `/etc/resolv.conf`, pointing it at quad100 (`100.100.100.100`).
- Tailnet names (`*.ts.net`) resolve inside tailscaled from the pushed network
  map — they never leave the device and have no TTL lag.
- Public names forward from quad100 to the tailnet global nameservers over DoH.
- A tailscaled restart or upgrade re-applies this programming; with global
  nameservers configured this is safe. Without them, a restart can convert a
  working host into a full DNS outage while existing TCP connections keep
  running, which presents as application-level "connection errors".
- Devices using an exit node resolve DNS through the exit node by default,
  regardless of global nameservers, so tailnet DNS changes do not alter
  Mullvad-style exit-node behavior.
- `serverpro server doctor` watches this posture: the provider `tailnet dns`
  check warns when MagicDNS is enabled with zero global nameservers, and the
  remote `dns resolution` canary separates resolver failure from egress
  failure.

### Verify on a server

```sh
tailscale dns status   # Resolvers must list the configured nameservers
dig google.com         # NOERROR via 100.100.100.100
```

## Official docs

- Tailscale API: <https://tailscale.com/docs/reference/tailscale-api>
- Tailscale SSH: <https://tailscale.com/kb/1193/tailscale-ssh>
- Auth keys: <https://tailscale.com/kb/1085/auth-keys>
- Tags: <https://tailscale.com/kb/1068/tags>
- ACL syntax: <https://tailscale.com/kb/1337/acl-syntax>
- Hetzner Tailscale install guide: <https://tailscale.com/docs/install/cloud/hetzner>
- DNS in Tailscale: <https://tailscale.com/docs/reference/dns-in-tailscale>
- MagicDNS: <https://tailscale.com/docs/features/magicdns>
- Linux DNS: <https://tailscale.com/docs/reference/linux-dns>
