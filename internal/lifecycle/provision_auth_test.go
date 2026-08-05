package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
)

func TestRunRejectsProvidedAuthKey(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	h := &fakeHetzner{}
	ts := &fakeTailscale{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{TSAuthKey: "tskey-auth-provided", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "namespace-scoped") {
		t.Fatalf("expected namespace-scoped auth key error, got %v", err)
	}
	if ts.created {
		t.Fatal("should not create auth key when only provided key is present")
	}
	if h.userData != "" {
		t.Fatal("should not render cloud-init with provided auth key")
	}
}

func TestBestDeviceIDPrefersNodeID(t *testing.T) {
	if got := bestDeviceID(tailscale.Device{ID: "393735751060", NodeID: "n1"}); got != "n1" {
		t.Fatalf("bestDeviceID() = %q", got)
	}
}

func TestBestNameFallsBackToHostname(t *testing.T) {
	if got := bestName(tailscale.Device{Hostname: "prod-host"}); got != "prod-host" {
		t.Fatalf("bestName() = %q", got)
	}
}
