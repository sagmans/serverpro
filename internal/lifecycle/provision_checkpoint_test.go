package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	if st.Project != "prod" || st.Server != "web" || st.Compute.ID != "" || st.Cloudflare.TunnelID != "tun1" {
		t.Fatalf("checkpoint lost created tunnel: %+v", st)
	}
}

func TestRunCheckpointsComputeIntentBeforeExternalMutation(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	path := provisionStatePath(t)
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{createErr: errors.New("tunnel failed")}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "tunnel failed") {
		t.Fatalf("expected tunnel failure, got %v", err)
	}
	st, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatalf("state missing: %v", loadErr)
	}
	if st.Compute.Provider != "hetzner" || st.Compute.Namespace != "prod" || st.Compute.Server != "web" || st.Compute.Name != cfg.Compute.Name || st.Compute.Location != cfg.Compute.Location || st.Compute.Size != cfg.Compute.Size || st.Compute.Image != cfg.Compute.Image {
		t.Fatalf("compute intent was not checkpointed before external mutation: %+v", st.Compute)
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
	if st.Cloudflare.TunnelID != "tun1" || st.Tailscale.AuthKeyID != "k1" || st.Compute.ProviderState["access_policy_id"] != "" || st.Compute.ID != "" {
		t.Fatalf("checkpoint changed wrong resources: %+v", st)
	}
}

func TestRunSavesFirewallCreatedBeforeServerFailure(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	path := provisionStatePath(t)
	h := &fakeHetzner{serverErr: errors.New("server failed")}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: h, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "server failed") {
		t.Fatalf("expected server failure, got %v", err)
	}
	st, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatalf("state missing: %v", loadErr)
	}
	if st.Cloudflare.TunnelID != "tun1" || st.Compute.ProviderState["access_policy_id"] != "1" || st.Compute.ID != "" {
		t.Fatalf("checkpoint lost created access policy: %+v", st)
	}
}

func TestRunSavesCreatedResourcesOnWaitFailure(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	path := provisionStatePath(t)
	h := &fakeHetzner{waitErr: errors.New("wait failed")}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: h, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "wait failed") {
		t.Fatalf("expected wait failure, got %v", err)
	}
	st, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatalf("state missing: %v", loadErr)
	}
	if st.Cloudflare.TunnelID != "tun1" || st.Compute.ProviderState["access_policy_id"] != "1" || st.Compute.ID != "2" {
		t.Fatalf("checkpoint lost created resources: %+v", st)
	}
}
