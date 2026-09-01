# Hetzner setup

Use the official Hetzner Cloud docs for account, token, server, firewall, and DNS
behavior.

## Before using serverpro

1. Create or select a Hetzner Cloud project.
2. Create a Hetzner Cloud API token for that project.
3. Keep the token private; serverpro stores it only in the selected server's
   credential file.
4. Use `serverpro location list`, `serverpro size list`, and `serverpro image
   list -p hetzner` to choose supported locations, server types, and images
   before creating a server.

serverpro creates a provider firewall for managed servers and keeps public SSH
closed. DNS records are not managed by serverpro.

## Official docs

- Hetzner Cloud API: <https://docs.hetzner.cloud/>
- Servers API: <https://docs.hetzner.cloud/reference/cloud#servers>
- Firewalls API: <https://docs.hetzner.cloud/reference/cloud#firewalls>
- Images API: <https://docs.hetzner.cloud/reference/cloud#images>
- Locations API: <https://docs.hetzner.cloud/reference/cloud#locations>
- Server types API: <https://docs.hetzner.cloud/reference/cloud#server-types>
- DNS: <https://www.hetzner.com/dns/>
- VNC console: <https://docs.hetzner.com/cloud/servers/getting-started/vnc-console>
- Rescue system: <https://docs.hetzner.com/cloud/servers/getting-started/rescue-system/>
