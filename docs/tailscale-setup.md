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

## Recommended grants policy

serverpro writes only its tag owners and one `ssh` rule to the tailnet policy
file. It never edits the packet rules (`acls` or `grants`). Those rules decide
which tailnet devices reach which ports on a managed server. A new tailnet
starts with the default allow-all rule, so any tailnet device, including a
compromised laptop, reaches every port that a server listens on. UFW is then the
only barrier.

Replace the allow-all rule with grants that open only what each source needs.
Example for the default tag `tag:serverpro-server` (use your namespace tag,
`tag:serverpro-<namespace>`, where it applies):

```jsonc
{
  "tagOwners": {
    "tag:serverpro-server": ["autogroup:admin"],
  },
  "grants": [
    // Each member reaches their own untagged devices.
    {"src": ["autogroup:member"], "dst": ["autogroup:self"], "ip": ["*"]},
    // Members may use exit nodes; drop this grant if you run none.
    {"src": ["autogroup:member"], "dst": ["autogroup:internet"], "ip": ["*"]},
    // Tailscale SSH and the ports Tailscale Serve publishes on managed servers.
    {"src": ["autogroup:member"], "dst": ["tag:serverpro-server"], "ip": ["tcp:22", "tcp:443"]},
  ],
  "ssh": [
    // serverpro writes and reconciles this rule; keep it.
    {"action": "check", "src": ["autogroup:admin"], "dst": ["tag:serverpro-server"], "users": ["<admin user>"]},
  ],
  "tests": [
    // Use a real member login. Tailscale rejects a change that breaks this.
    {
      "src": "<member login>",
      "accept": ["tag:serverpro-server:22", "tag:serverpro-server:443"],
      "deny": ["tag:serverpro-server:5432"],
    },
  ],
}
```

- Tailscale SSH needs both a network grant to `tcp:22` and an `ssh` rule. The
  `ssh` rule alone does not open the port, and the network grant alone does
  not let anyone log in: the `ssh` rule still limits that to its sources.
- List only the ports that Tailscale Serve publishes on the server
  (`tailscale serve status`). `443` is the Serve HTTPS default; add each other
  Serve port, for example `tcp:8443`.
- Write `tcp:22`, not `22`. A port without a protocol also opens UDP and ICMP.
- Never use a serverpro tag as a grant `src`. A managed server does not need
  to start connections to other tailnet devices, so a compromised server then
  reaches none of them.
- Keep an existing CI or operator grant to `tcp:22` when that source runs
  serverpro or deploys over SSH.
- With the `tests` block, Tailscale rejects any policy file change that
  closes an `accept` port or opens a `deny` port. List in `deny` the server
  ports that must stay closed to the tailnet, so a later edit cannot open
  them silently.
- Add grants for the traffic between your other devices before you remove the
  default rule. Otherwise those devices lose access to each other.
- Remove the default `{"src": ["*"], "dst": ["*"], "ip": ["*"]}` grant and the
  default `{"action": "accept", "src": ["*"], "dst": ["*:*"]}` acl.

`serverpro server doctor` reads the policy file with the Tailscale API token
and the provider `tailnet policy` check warns when:

- the file has no `acls` and no `grants` section (Tailscale then applies its
  default allow-all policy);
- an `acls` rule accepts `*` to `*:*`;
- a grant gives `*` to `*` on ip `*`.

Doctor reads each "everything" spelling the same way. A source of `*`,
`autogroup:member`, or a CIDR that covers the whole tailnet range
(`100.64.0.0/10` or `fd7a:115c:a1e0::/48`, so also `0.0.0.0/0` and `::/0`)
is every source. A host of `*`, `autogroup:tagged` (serverpro tags every
server), or such a CIDR (also in brackets in an acl) is every host. A
`hosts` alias counts as the address or range it names, and the warning shows
both. Ports of `*`, `0-65535`, or `1-65535` (also inside an acl port list)
are every port, and so is a grant `ip` of `tcp:*`, `udp:*`, `6:*`, or `17:*`
(or a full range after the protocol). So
`{"src": ["autogroup:member"], "dst": ["autogroup:tagged"], "ip": ["tcp:*"]}`
warns like the default grant. A part of the tailnet range, a single
address, and `icmp:*` do not warn.

An empty `"acls": []` denies all traffic, so doctor does not warn on it.
Doctor reads a file with `grants` and no `acls` as grants only. Tailscale's
docs do not say whether the default allow-all still applies to such a file:
check the effective access in the admin console before you rely on it.

## Official docs

- Tailscale API: <https://tailscale.com/docs/reference/tailscale-api>
- Tailscale SSH: <https://tailscale.com/kb/1193/tailscale-ssh>
- Auth keys: <https://tailscale.com/kb/1085/auth-keys>
- Tags: <https://tailscale.com/kb/1068/tags>
- ACL syntax: <https://tailscale.com/kb/1337/acl-syntax>
- Grants syntax: <https://tailscale.com/docs/reference/syntax/grants>
- Policy file syntax: <https://tailscale.com/docs/reference/syntax/policy-file>
- ACL examples (allow all, deny all): <https://tailscale.com/docs/reference/examples/acls>
- Hetzner Tailscale install guide: <https://tailscale.com/docs/install/cloud/hetzner>
- DNS in Tailscale: <https://tailscale.com/docs/reference/dns-in-tailscale>
- MagicDNS: <https://tailscale.com/docs/features/magicdns>
- Linux DNS: <https://tailscale.com/docs/reference/linux-dns>
