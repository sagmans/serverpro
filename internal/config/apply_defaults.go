package config

import (
	"strings"

	"github.com/assagman/serverpro/internal/ownership"
)

func ApplyDefaults(c *Config) {
	applyDefaults(c)
}

func applyDefaults(c *Config) {
	c.normalizeNamespace()
	d := Default()
	applyServerDefaults(c, d)
	applyCredentialDefaults(c, d)
	applyComputeDefaults(c, d)
	applyAdminDefaults(c, d)
	applyNetworkDefaults(c, d)
	applyAccessDefaults(c, d)
	applyCloudflareDefaults(c)
	applyHardeningDefaults(c, d)
	c.normalizeNamespace()
}

func (c *Config) normalizeNamespace() {
	if c.Project == "" {
		c.Project = c.Namespace
	}
	if c.Namespace == "" {
		c.Namespace = c.Project
	}
}

func applyServerDefaults(c *Config, d Config) {
	if c.Server == "" {
		c.Server = d.Server
	}
}

func applyCredentialDefaults(c *Config, d Config) {
	if c.Credentials.Mode == "" {
		c.Credentials.Mode = d.Credentials.Mode
	}
	if c.Credentials.JSONPath == "" || c.Credentials.JSONPath == defaultCredentialsJSONPath {
		c.Credentials.JSONPath = ServerCredentialsPath(c.Project, c.Server)
	}
}

func applyComputeDefaults(c *Config, d Config) {
	if c.Compute.Name == "" {
		c.Compute.Name = ServerResourceName(c.Project, c.Server)
	}
	if c.Compute.Labels == nil {
		c.Compute.Labels = d.Compute.Labels
	}
	c.Compute.Labels = ownership.ConfigLabels(c.Project, c.Server, c.Compute.Labels)
}

func applyAdminDefaults(c *Config, d Config) {
	// WHY: leave empty usernames empty so import/ssh can detect "not saved" and prompt
	// instead of silently inventing deploy after recovery.
	_ = d
}

func applyNetworkDefaults(c *Config, d Config) {
	if c.Network.Ingress == "" {
		c.Network.Ingress = d.Network.Ingress
	}
	if c.Network.Egress.Mode == "" {
		c.Network.Egress.Mode = d.Network.Egress.Mode
	}
}

func applyAccessDefaults(c *Config, d Config) {
	if !c.Access.Tailscale.Enabled && !c.Access.Tailscale.SSH && len(c.Access.Tailscale.Tags) == 0 {
		c.Access.Tailscale = d.Access.Tailscale
	}
	if c.Project != "" && hasDefaultTailscaleTags(c.Access.Tailscale.Tags) {
		c.Access.Tailscale.Tags = []string{ProjectTailscaleTag(c.Project)}
	}
	if c.Access.Tailscale.Tailnet == "" {
		c.Access.Tailscale.Tailnet = d.Access.Tailscale.Tailnet
	}
	if c.Access.Tailscale.RootPolicy == "" {
		c.Access.Tailscale.RootPolicy = d.Access.Tailscale.RootPolicy
	}
}

func hasDefaultTailscaleTags(tags []string) bool {
	return len(tags) == 0 || strings.Join(tags, ",") == "tag:serverpro-server"
}

func applyCloudflareDefaults(c *Config) {
	if c.Cloudflare.Tunnel.Name == "" && c.Compute.Name != "" {
		c.Cloudflare.Tunnel.Name = c.Compute.Name
	}
}

func applyHardeningDefaults(c *Config, d Config) {
	if c.Hardening.Profile == "" {
		c.Hardening = d.Hardening
	}
}
