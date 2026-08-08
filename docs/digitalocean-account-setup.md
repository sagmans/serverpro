# DigitalOcean setup

Use the official DigitalOcean docs for account, token, droplet, firewall, DNS,
and recovery behavior.

## Before using serverpro

1. Create or select a DigitalOcean account/project.
2. Create an API token with access to droplets, firewalls, tags, regions, sizes,
   and images.
3. Keep the token private; serverpro stores it only in the selected server's
   credential file.
4. Use `serverpro catalog ... -p digitalocean` to choose supported regions,
   sizes, and image slugs before creating a server.

serverpro creates IPv4-backed droplets, ensures ownership tags, creates a
tag-bound firewall, and submits bootstrap data as droplet user data. DNS records
are not managed by serverpro. Import can recover a historical serverpro firewall
using the complete legacy ownership-tag selector set only when full live Droplet
inventory proves those broad selectors match no unrelated resource.

## Official docs

- DigitalOcean API: <https://docs.digitalocean.com/reference/api/>
- Droplets API: <https://docs.digitalocean.com/reference/api/reference/droplets/>
- Droplet user data: <https://docs.digitalocean.com/products/droplets/how-to/provide-user-data/>
- Firewalls API: <https://docs.digitalocean.com/reference/api/reference/firewalls/>
- Tags API: <https://docs.digitalocean.com/reference/api/reference/tags/>
- Regions API: <https://docs.digitalocean.com/reference/api/reference/regions/>
- Sizes API: <https://docs.digitalocean.com/reference/api/reference/sizes/>
- Images API: <https://docs.digitalocean.com/reference/api/reference/images/>
- Domain records API: <https://docs.digitalocean.com/reference/api/reference/domain-records/>
- Recovery console: <https://docs.digitalocean.com/products/droplets/how-to/recovery/recovery-console/>
