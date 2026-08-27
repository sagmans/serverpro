package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunEnsuresTailscalePolicyBeforeAuthKeyAndTracksManagedPolicy(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	ts := &fakeTailscale{}
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ts.calls, ","); !strings.HasPrefix(got, "ensure-policy,create-key") {
		t.Fatalf("tailscale calls = %s", got)
	}
	if st.Tailscale.Tailnet != cfg.Access.Tailscale.Tailnet || !st.Tailscale.PolicySSHRule || strings.Join(st.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-prod" || strings.Join(st.Tailscale.PolicySSHTags, ",") != "tag:serverpro-prod" {
		t.Fatalf("managed policy state missing: %+v", st.Tailscale)
	}
}

func TestRunCreatesJITAuthKeyAndNeverStoresSecrets(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	h := &fakeHetzner{}
	ts := &fakeTailscale{}
	r := &fakeRemote{}
	statePath := provisionStatePath(t)
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: testTailscaleAPIToken, Cloudflare: testCloudflareAPIToken}, StatePath: statePath, Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: r}})
	if err != nil {
		t.Fatal(err)
	}
	if !ts.created || !strings.Contains(strings.Join(ts.calls, ","), "delete-key") {
		t.Fatalf("expected JIT auth key create/delete calls, got %v", ts.calls)
	}
	if !strings.Contains(h.userData, "tskey-auth-created") {
		t.Fatal("cloud-init missing JIT auth key")
	}
	if !strings.Contains(h.userData, testAdminPasswordHash) || strings.Contains(h.userData, "NOPASSWD") {
		t.Fatalf("cloud-init missing password-required sudo state:\n%s", h.userData)
	}
	if strings.Contains(h.userData, testTailscaleAPIToken) || strings.Contains(h.userData, testCloudflareAPIToken) {
		t.Fatal("cloud-init leaked provider token")
	}
	if st.Compute.ID != "2" || st.Tailscale.NodeID != "d1" || st.Tailscale.AuthKeyID != "" || st.Cloudflare.TunnelID != "tun1" {
		t.Fatalf("bad state: %+v", st)
	}
	if strings.Contains(st.Namespace, "tskey") {
		t.Fatal("state leaked auth key")
	}
	saved, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tailscale.AuthKeyID != "" {
		t.Fatalf("saved state retained auth key id: %+v", saved.Tailscale)
	}
}
