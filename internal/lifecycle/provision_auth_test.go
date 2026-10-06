package lifecycle

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
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

// A device recorded for an earlier server must not satisfy a fresh compute
// run: the new key's enrolment window decides instead.
func TestRunFreshComputeIgnoresLeftoverRecordedDevice(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "prod", Server: cfg.Server, Compute: state.ComputeState{Name: cfg.Compute.Name}, Tailscale: state.TailscaleState{Tailnet: cfg.Access.Tailscale.Tailnet, NodeID: "old-device"}}); err != nil {
		t.Fatal(err)
	}
	ts := &fakeTailscale{keyCreated: "2026-10-07T10:00:00Z"}
	opt := Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api-token", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}}
	if _, err := Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if len(ts.waitQueries) != 1 || ts.waitQueries[0].NodeID != "" || ts.waitQueries[0].CreatedNotBefore.IsZero() {
		t.Fatalf("fresh compute query = %+v", ts.waitQueries)
	}
}

// A key without a usable control-plane time must still open a bounded window;
// an unbounded one would let a stale online twin win before the node joins.
func TestAuthKeyCreatedAtFallsBackToBoundedLocalWindow(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if got := authKeyCreatedAt(tailscale.AuthKey{Created: "2026-10-07T11:59:00Z"}, now); !got.Equal(now.Add(-time.Minute)) {
		t.Fatalf("control-plane time = %s", got)
	}
	for _, created := range []string{"", "not-a-time"} {
		if got := authKeyCreatedAt(tailscale.AuthKey{Created: created}, now); !got.Equal(now.Add(-authKeyClockSkewMargin)) {
			t.Fatalf("fallback for %q = %s", created, got)
		}
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
