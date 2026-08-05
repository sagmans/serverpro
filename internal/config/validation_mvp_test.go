package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsPublicSSH(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Access.PublicSSH = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "public_ssh") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidEgressMode(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Network.Egress.Mode = "closed"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "network.egress.mode") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsRelaxedHardening(t *testing.T) {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Hardening.UFW = false
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "hardening") {
		t.Fatalf("Validate() error = %v", err)
	}
}
