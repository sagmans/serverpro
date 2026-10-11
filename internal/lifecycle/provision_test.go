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
	st, err := Run(context.Background(), Options{Config: cfg, AdminPassword: testAdminPassword, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
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
	st, err := Run(context.Background(), Options{Config: cfg, AdminPassword: testAdminPassword, Creds: credentials.Set{Tailscale: testTailscaleAPIToken, Cloudflare: testCloudflareAPIToken}, StatePath: statePath, Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: r}})
	if err != nil {
		t.Fatal(err)
	}
	if !ts.created || !strings.Contains(strings.Join(ts.calls, ","), "delete-key") {
		t.Fatalf("expected JIT auth key create/delete calls, got %v", ts.calls)
	}
	if !strings.Contains(h.userData, "tskey-auth-created") {
		t.Fatal("cloud-init missing JIT auth key")
	}
	if strings.Contains(h.userData, testAdminPassword) || strings.Contains(h.userData, "hashed_passwd") || !strings.Contains(h.userData, "lock_passwd: true") {
		t.Fatalf("cloud-init must create the admin locked, without password material:\n%s", h.userData)
	}
	setPassword := -1
	for i, script := range r.scripts {
		if script == AdminPasswordScript(cfg.Admin.Username) {
			setPassword = i
			break
		}
	}
	// The password must be set (closing the first-boot grant) before any other
	// remote bootstrap step; only the readiness probe may precede it.
	if setPassword != 1 || r.scripts[0] != "true" || len(r.inputs) != 1 || r.inputs[0] != AdminPasswordInput(testAdminPassword) {
		t.Fatalf("admin password not set first after readiness: scripts=%q inputs=%d", r.scripts, len(r.inputs))
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
