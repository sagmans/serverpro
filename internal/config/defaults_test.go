package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultCreateUsesNoPublicIngress(t *testing.T) {
	cfg := Default()
	if cfg.Cloudflare.Tunnel.Enabled || cfg.Network.Ingress != "none" {
		t.Fatalf("default ingress should be none: cloudflare=%+v network=%s", cfg.Cloudflare.Tunnel, cfg.Network.Ingress)
	}
}

func TestApplyDefaultsDerivesNamespaceScopedFields(t *testing.T) {
	dir := configTestHome(t)
	cfg := Config{Namespace: "prod"}
	ApplyDefaults(&cfg)

	if cfg.Server != DefaultServer() {
		t.Fatalf("server = %q", cfg.Server)
	}
	if cfg.Credentials.JSONPath != filepath.Join(dir, ".config", "serverpro", "namespaces", "prod", "servers", DefaultServer(), "credentials.json") {
		t.Fatalf("credentials path = %q", cfg.Credentials.JSONPath)
	}
	if got := strings.Join(cfg.Access.Tailscale.Tags, ","); got != "tag:serverpro-prod" {
		t.Fatalf("tailscale tags = %q", got)
	}
	if cfg.Compute.Location != "" || cfg.Compute.Size != "" || cfg.Compute.Image != "" {
		t.Fatalf("catalog selections must stay explicit: %+v", cfg.Compute)
	}
	if cfg.Admin.Username != "" {
		t.Fatalf("admin username must stay explicit: %q", cfg.Admin.Username)
	}
	if cfg.Compute.Labels["managed-by"] != "serverpro" || cfg.Compute.Labels["serverpro.namespace"] != "prod" || cfg.Compute.Labels["serverpro.server"] != DefaultServer() {
		t.Fatalf("labels = %#v", cfg.Compute.Labels)
	}
}

func TestApplyDefaultsPreservesExplicitCustomFields(t *testing.T) {
	cfg := Config{
		Namespace: "prod",
		Server:    "api",
		Credentials: Credentials{
			JSONPath: "~/.config/serverpro/namespaces/prod/servers/api/credentials.json",
		},
		Compute: Compute{
			Location: "nbg1",
			Size:     "cpx31",
			Image:    "debian-12",
			Labels:   map[string]string{"custom": "yes"},
		},
		Access: Access{Tailscale: Tailscale{
			Enabled:    true,
			SSH:        true,
			Tailnet:    "example.ts.net",
			Tags:       []string{"tag:serverpro-prod-api"},
			RootPolicy: "disabled",
		}},
		Cloudflare: Cloudflare{Tunnel: TunnelConfig{Name: "custom-tunnel"}},
		Hardening:  Hardening{Profile: "custom"},
	}

	ApplyDefaults(&cfg)

	if cfg.Credentials.JSONPath != "~/.config/serverpro/namespaces/prod/servers/api/credentials.json" {
		t.Fatalf("credentials path = %q", cfg.Credentials.JSONPath)
	}
	if cfg.Compute.Location != "nbg1" || cfg.Compute.Size != "cpx31" || cfg.Compute.Image != "debian-12" {
		t.Fatalf("compute defaults overwritten: %+v", cfg.Compute)
	}
	if cfg.Compute.Labels["custom"] != "yes" || cfg.Compute.Labels["serverpro.namespace"] != "prod" || cfg.Compute.Labels["serverpro.server"] != "api" {
		t.Fatalf("labels = %#v", cfg.Compute.Labels)
	}
	if got := strings.Join(cfg.Access.Tailscale.Tags, ","); got != "tag:serverpro-prod-api" {
		t.Fatalf("tailscale tags = %q", got)
	}
	if cfg.Access.Tailscale.Tailnet != "example.ts.net" || cfg.Access.Tailscale.RootPolicy != "disabled" {
		t.Fatalf("tailscale overwritten: %+v", cfg.Access.Tailscale)
	}
	if cfg.Cloudflare.Tunnel.Name != "custom-tunnel" {
		t.Fatalf("tunnel name = %q", cfg.Cloudflare.Tunnel.Name)
	}
	if cfg.Hardening.Profile != "custom" {
		t.Fatalf("hardening = %+v", cfg.Hardening)
	}
}
