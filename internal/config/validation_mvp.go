package config

import "errors"

func (c Config) validateMVPConstraints() error {
	if !c.Access.Tailscale.Enabled || !c.Access.Tailscale.SSH {
		return errors.New("access.tailscale.enabled and ssh must stay true for MVP")
	}
	if c.Cloudflare.Tunnel.Enabled && (!c.Cloudflare.Tunnel.CreateConnectorOnly || c.Cloudflare.Tunnel.SmokeRoute.Enabled) {
		return errors.New("cloudflare.tunnel must be connector-only and smoke_route disabled when enabled")
	}
	if c.Admin.StoreConsolePassword {
		return errors.New("admin.store_console_password is not implemented in MVP")
	}
	if c.Access.PublicSSH {
		return errors.New("access.public_ssh must stay false for MVP")
	}
	if c.Access.EmergencySSH.Enabled {
		return errors.New("access.emergency_ssh is instruction-only; use provider rescue console guidance")
	}
	if c.Network.Ingress != "none" && c.Network.Ingress != "cloudflare-tunnel" {
		return errors.New("network.ingress must be none or cloudflare-tunnel")
	}
	if c.Network.Egress.Mode != "restricted" && c.Network.Egress.Mode != "open" {
		return errors.New("network.egress.mode must be restricted or open")
	}
	if c.Hardening.Profile != "strict" || !c.Hardening.UnattendedUpgrades || !c.Hardening.AppArmor || !c.Hardening.UFW || !c.Hardening.JournaldPersistent {
		return errors.New("hardening strict profile controls must stay enabled for MVP")
	}
	if c.Access.Tailscale.RootPolicy != "check-or-disabled" {
		return errors.New("access.tailscale.root_policy must be check-or-disabled")
	}
	return nil
}
