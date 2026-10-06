package lifecycle

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestRunRejectsProvidedAuthKeyWithAPIToken pins the single rule for stored
// user-supplied keys: they are refused whether or not an API token can mint a
// short-lived replacement, so one credentials file never behaves two ways.
func TestRunRejectsProvidedAuthKeyWithAPIToken(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	h := &fakeHetzner{}
	ts := &fakeTailscale{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api-token", TSAuthKey: "tskey-auth-provided", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "namespace-scoped") {
		t.Fatalf("expected namespace-scoped auth key error, got %v", err)
	}
	if ts.created {
		t.Fatal("should not create a replacement key while a user-supplied key is stored")
	}
	if h.userData != "" {
		t.Fatal("should not render cloud-init with a user-supplied auth key")
	}
}

// TestRunBindsDeviceToBootstrapKeyThenRecordedID pins how create chooses the
// node that receives bootstrap secrets: the first bind only accepts devices
// enrolled after the single-use key, and a rerun keeps the recorded device.
func TestRunBindsDeviceToBootstrapKeyThenRecordedID(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	ts := &fakeTailscale{keyCreated: "2026-10-07T10:00:00Z"}
	path := provisionStatePath(t)
	opt := Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api-token", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}}

	st, err := Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	if len(ts.waitQueries) != 1 || !ts.waitQueries[0].CreatedNotBefore.Equal(want) || ts.waitQueries[0].NodeID != "" {
		t.Fatalf("first bind query = %+v", ts.waitQueries)
	}
	if st.Tailscale.NodeID != "d1" || !st.Tailscale.AuthKeyCreatedAt.IsZero() {
		t.Fatalf("bound state = %+v", st.Tailscale)
	}

	if _, err := Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if len(ts.waitQueries) != 2 || ts.waitQueries[1].NodeID != "d1" {
		t.Fatalf("rerun query = %+v", ts.waitQueries)
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
