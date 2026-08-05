# Cloudflare Tunnel setup

Cloudflare Tunnel support is optional. Current serverpro ingress commands record
connector/local-route metadata only; they do not yet publish public DNS routes.

## Before using serverpro

1. Create or select a Cloudflare account.
2. Create an API token with Cloudflare Tunnel access for the account.
3. Add DNS permissions only when future route publishing needs them.
4. Keep the token private; serverpro stores it only in the selected server's
   credential file.

Cloudflare Tunnel does not require inbound provider firewall openings. Restrict
outbound firewall rules according to Cloudflare's tunnel firewall guidance when
hardening beyond the MVP defaults.

## Official docs

- Cloudflare API: <https://developers.cloudflare.com/api/>
- Create remote tunnel API: <https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel-api/>
- Tunnel tokens: <https://developers.cloudflare.com/tunnel/advanced/tunnel-tokens/>
- Tunnel routing: <https://developers.cloudflare.com/tunnel/routing/>
- Tunnel firewall guidance: <https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-with-firewall/>
- DNS records API: <https://developers.cloudflare.com/api/resources/dns/subresources/records/>
- cloudflared downloads: <https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/>
