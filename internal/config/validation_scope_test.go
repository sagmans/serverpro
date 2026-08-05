package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsUnscopedCredentialsPath(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Credentials.JSONPath = "~/.config/prod/shared/credentials.json"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "credentials.json_path") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsCrossNamespaceTailscaleTag(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Access.Tailscale.Tags = []string{"tag:serverpro-server"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "access.tailscale.tags") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidTailscaleTagSyntax(t *testing.T) {
	cfg := Example("example.com")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Access.Tailscale.Tags = []string{"tag:serverpro-example.com"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "invalid tailscale tag") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsCrossNamespaceTunnelName(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Name = "shared-01"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "cloudflare.tunnel.name") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCredentialsPathScopedToServerAcceptsOnlyServerDirectoryLayout(t *testing.T) {
	if !CredentialsPathScopedToServer("~/.config/serverpro/namespaces/prod/servers/web/credentials.json", "prod", "web") {
		t.Fatal("server credentials path should be scoped")
	}
	for _, path := range []string{
		"~/.config/serverpro/prod.json",
		"~/.config/serverpro/projects/prod/servers/web/credentials.json",
		"~/.config/serverpro/namespaces/prod/credentials.json",
		"~/.config/serverpro/namespaces/other/servers/web/credentials.json",
		"~/.config/serverpro/namespaces/prod/servers/api/credentials.json",
	} {
		if CredentialsPathScopedToServer(path, "prod", "web") {
			t.Fatalf("%q should not be scoped to server", path)
		}
	}
}
