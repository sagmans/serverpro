package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsProviderInvalidResourceNames(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Compute.Name = "prod_web"
	cfg.Cloudflare.Tunnel.Name = "prod-web"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "compute.name") {
		t.Fatalf("expected compute.name error, got %v", err)
	}

	cfg = Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Name = "prod_web"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "cloudflare.tunnel.name") {
		t.Fatalf("expected cloudflare.tunnel.name error, got %v", err)
	}
}

func TestValidateRejectsUppercaseNamespaceID(t *testing.T) {
	cfg := Example("Prod")
	cfg.Cloudflare.AccountID = "acc"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidAdminUsername(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Admin.Username = "root.user"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "invalid admin username") {
		t.Fatalf("Validate() error = %v", err)
	}
}
