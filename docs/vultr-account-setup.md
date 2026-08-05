# Vultr setup

Use the official Vultr docs for account, token, instance, firewall, DNS, and
console behavior.

## Before using serverpro

1. Create or select a Vultr account/project.
2. Create a Vultr API token.
3. Keep the token private; serverpro stores it only in the selected server's
   credential file.
4. Use `serverpro catalog ... -p vultr` to choose supported regions, plans, and
   OS IDs before creating a server.

serverpro creates IPv4-backed instances, attaches a deny-inbound firewall group,
and submits bootstrap data through cloud-init/user data. DNS records are not
managed by serverpro.

## Official docs

- Vultr API: <https://www.vultr.com/api/>
- Instances API: <https://www.vultr.com/api/#tag/instances>
- Plans API: <https://www.vultr.com/api/#tag/plans>
- Regions API: <https://www.vultr.com/api/#tag/region>
- Operating systems API: <https://www.vultr.com/api/#tag/os>
- Cloud-init: <https://docs.vultr.com/products/compute/instances/cloud-compute/features/cloud-init>
- Firewall groups: <https://docs.vultr.com/products/network/firewall-groups/>
- DNS: <https://docs.vultr.com/products/network/dns/>
- Web console: <https://docs.vultr.com/products/compute/cloud-compute/connection/vultr-console>
