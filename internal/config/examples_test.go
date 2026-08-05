package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExampleValidAfterRequiredFields(t *testing.T) {
	dir := configTestHome(t)
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Server != DefaultServer() {
		t.Fatalf("server default = %q", cfg.Server)
	}
	if cfg.Access.Tailscale.Tailnet != "-" {
		t.Fatalf("tailnet default = %q", cfg.Access.Tailscale.Tailnet)
	}
	if cfg.Compute.Labels["managed-by"] != "serverpro" {
		t.Fatal("managed-by label missing")
	}
	if cfg.Compute.Labels["serverpro.server"] != DefaultServer() || cfg.Compute.Labels["serverpro.namespace"] != "prod" {
		t.Fatalf("ownership labels = %#v", cfg.Compute.Labels)
	}
	if cfg.Credentials.JSONPath != filepath.Join(dir, ".config", "serverpro", "namespaces", "prod", "servers", DefaultServer(), "credentials.json") {
		t.Fatalf("credentials path = %q", cfg.Credentials.JSONPath)
	}
	if got := strings.Join(cfg.Access.Tailscale.Tags, ","); got != "tag:serverpro-prod" {
		t.Fatalf("tailscale tags = %q", got)
	}
}

func TestExampleServerDefaultsNamedServer(t *testing.T) {
	cfg := ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Server != "web" {
		t.Fatalf("server = %q", cfg.Server)
	}
	if cfg.Compute.Name != "prod-web" {
		t.Fatalf("server name = %q", cfg.Compute.Name)
	}
	if cfg.Cloudflare.Tunnel.Name != "prod-web" {
		t.Fatalf("tunnel name = %q", cfg.Cloudflare.Tunnel.Name)
	}
	if cfg.Compute.Labels["serverpro.server"] != "web" || cfg.Compute.Labels["serverpro.namespace"] != "prod" {
		t.Fatalf("ownership labels = %#v", cfg.Compute.Labels)
	}
}

func TestExampleServerDefaultsEmptyServer(t *testing.T) {
	cfg := ExampleServer("prod", "")
	if cfg.Server != DefaultServer() {
		t.Fatalf("server = %q", cfg.Server)
	}
	if cfg.Compute.Name != "prod-01" {
		t.Fatalf("server name = %q", cfg.Compute.Name)
	}
	if cfg.Compute.Labels["serverpro.server"] != DefaultServer() || cfg.Compute.Labels["serverpro.namespace"] != "prod" {
		t.Fatalf("ownership labels = %#v", cfg.Compute.Labels)
	}
}
