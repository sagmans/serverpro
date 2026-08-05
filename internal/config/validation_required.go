package config

import (
	"fmt"
	"strings"
)

func (c Config) validateRequired() error {
	var missing []string
	if c.Project == "" {
		missing = append(missing, "namespace")
	}
	if c.Server == "" {
		missing = append(missing, "server")
	}
	if c.Credentials.JSONPath == "" {
		missing = append(missing, "credentials.json_path")
	}
	if c.Compute.Name == "" {
		missing = append(missing, "compute.name")
	}
	if c.Compute.Location == "" {
		missing = append(missing, "compute.location")
	}
	if c.Compute.Size == "" {
		missing = append(missing, "compute.size")
	}
	if c.Compute.Image == "" {
		missing = append(missing, "compute.image")
	}
	if c.Admin.Username == "" {
		missing = append(missing, "admin.username")
	}
	if c.Access.Tailscale.Enabled && c.Access.Tailscale.Tailnet == "" {
		missing = append(missing, "access.tailscale.tailnet")
	}
	if c.Access.Tailscale.Enabled && len(c.Access.Tailscale.Tags) == 0 {
		missing = append(missing, "access.tailscale.tags")
	}
	if c.Cloudflare.Tunnel.Enabled && c.Cloudflare.AccountID == "" {
		missing = append(missing, "cloudflare.account_id")
	}
	if c.Cloudflare.Tunnel.Enabled && c.Cloudflare.Tunnel.Name == "" {
		missing = append(missing, "cloudflare.tunnel.name")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}
