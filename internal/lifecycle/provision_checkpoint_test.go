package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunSavesTunnelCreatedBeforeTailscaleFailure(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	path := provisionStatePath(t)
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: &fakeTailscale{keyErr: context.Canceled}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil {
		t.Fatal("expected tailscale error")
	}
	st, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatalf("state missing: %v", loadErr)
	}
	if st.Namespace != "prod" || st.Server != "web" || st.Compute.ID != "" || st.Cloudflare.TunnelID != "tun1" {
		t.Fatalf("checkpoint lost created tunnel: %+v", st)
	}
}

func TestRunSavesTunnelCreatedBeforeFirewallFailure(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	path := provisionStatePath(t)
	h := &fakeHetzner{firewallErr: errors.New("firewall failed")}
	ts := &fakeTailscale{deleteErr: errors.New("auth key delete failed")}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "firewall failed") {
		t.Fatalf("expected firewall failure, got %v", err)
	}
	st, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatalf("state missing: %v", loadErr)
	}
	if _, found := compute.ManagedResourceID(st.Compute.ManagedResources, compute.ManagedResourceAccessPolicy); st.Cloudflare.TunnelID != "tun1" || st.Tailscale.AuthKeyID != "k1" || found || st.Compute.ID != "" {
		t.Fatalf("checkpoint changed wrong resources: %+v", st)
	}
}

func TestRunCheckpointsComputeResourcesBeforeLateProvisionFailure(t *testing.T) {
	tests := []struct {
		name      string
		compute   *fakeHetzner
		wantError string
		wantID    string
	}{
		{name: "server create", compute: &fakeHetzner{serverErr: errors.New("server failed")}, wantError: "server failed"},
		{name: "server wait", compute: &fakeHetzner{waitErr: errors.New("wait failed")}, wantError: "wait failed", wantID: "2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.ExampleServer("prod", "web")
			cfg.Cloudflare.AccountID = "acc"
			cfg.Cloudflare.Tunnel.Enabled = true
			path := provisionStatePath(t)
			_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: test.compute, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected %q failure, got %v", test.wantError, err)
			}
			st, loadErr := state.Load(path)
			if loadErr != nil {
				t.Fatalf("state missing: %v", loadErr)
			}
			policyID, found := compute.ManagedResourceID(st.Compute.ManagedResources, compute.ManagedResourceAccessPolicy)
			if st.Cloudflare.TunnelID != "tun1" || !found || policyID != "1" || len(st.Compute.ProviderState) != 0 || st.Compute.ID != test.wantID {
				t.Fatalf("checkpoint lost created resources: %+v", st)
			}
		})
	}
}
