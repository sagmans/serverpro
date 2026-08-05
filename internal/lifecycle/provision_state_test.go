package lifecycle

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunCreatesCloudflareTunnelBeforeHetznerResources(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	h := &fakeHetzner{}
	ts := &fakeTailscale{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{createErr: errors.New("forbidden")}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected cloudflare error, got %v", err)
	}
	if h.firewallCreated || ts.created {
		t.Fatal("hetzner/tailscale flow started before cloudflare write permission was proven")
	}
}

func TestInitializeProvisionStateReturnsStatErrors(t *testing.T) {
	_, err := initializeProvisionState("invalid\x00state", config.ExampleServer("prod", "web"))
	if err == nil {
		t.Fatal("expected state stat error")
	}
}

func TestInitializeProvisionStateDoesNotRewriteCurrentState(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "prod", Server: "web", Compute: state.ComputeState{Name: cfg.Compute.Name}, Tailscale: state.TailscaleState{Tailnet: cfg.Access.Tailscale.Tailnet}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initializeProvisionState(path, cfg); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("current state was rewritten")
	}
}

func TestInitializeProvisionStateMigratesMissingTailnet(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Access.Tailscale.Tailnet = "example.ts.net"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "prod", Server: "web", Compute: state.ComputeState{Name: cfg.Compute.Name}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}}); err != nil {
		t.Fatal(err)
	}
	got, err := initializeProvisionState(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tailscale.Tailnet != cfg.Access.Tailscale.Tailnet || saved.Tailscale.Tailnet != cfg.Access.Tailscale.Tailnet {
		t.Fatalf("tailnet migration missing: got=%+v saved=%+v", got.Tailscale, saved.Tailscale)
	}
}

func TestInitializeProvisionStateMigratesTokenDefaultTailnet(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Access.Tailscale.Tailnet = "example.ts.net"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "prod", Server: "web", Compute: state.ComputeState{Name: cfg.Compute.Name}, Tailscale: state.TailscaleState{Tailnet: "-"}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}}); err != nil {
		t.Fatal(err)
	}
	got, err := initializeProvisionState(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tailscale.Tailnet != cfg.Access.Tailscale.Tailnet {
		t.Fatalf("token-relative tailnet not migrated: %+v", got.Tailscale)
	}
}

func TestInitializeProvisionStateRejectsTailnetConflict(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Access.Tailscale.Tailnet = "expected.ts.net"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "prod", Server: "web", Compute: state.ComputeState{Name: cfg.Compute.Name}, Tailscale: state.TailscaleState{Tailnet: "other.ts.net"}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}}); err != nil {
		t.Fatal(err)
	}
	if _, err := initializeProvisionState(path, cfg); err == nil || !strings.Contains(err.Error(), "tailnet") {
		t.Fatalf("tailnet conflict accepted: %v", err)
	}
}

func TestSaveProvisionStatePreservesConcurrentIngressAndStatus(t *testing.T) {
	path := provisionStatePath(t)
	current := state.State{
		Namespace: "prod",
		Server:    "web",
		Compute: state.ComputeState{
			ID:         "server-1",
			PublicIPv4: "203.0.113.20",
		},
		Ingress: []state.IngressState{{Type: "cloudflare-tunnel", Hostname: "app.example.com"}},
	}
	if err := state.Save(path, current); err != nil {
		t.Fatal(err)
	}
	staleCheckpoint := current
	staleCheckpoint.Compute.PublicIPv4 = "203.0.113.10"
	staleCheckpoint.Ingress = nil
	staleCheckpoint.Tailscale.NodeID = "node-1"
	if err := saveProvisionState(path, staleCheckpoint); err != nil {
		t.Fatal(err)
	}
	got, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compute.PublicIPv4 != "203.0.113.20" || len(got.Ingress) != 1 {
		t.Fatalf("concurrent updates lost: %+v", got)
	}
	if got.Tailscale.NodeID != "node-1" {
		t.Fatalf("checkpoint mutation missing: %+v", got.Tailscale)
	}
}

func TestRunRejectsExistingStateForDifferentTarget(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "other", Server: "web"}); err != nil {
		t.Fatal(err)
	}
	h := &fakeHetzner{}
	ts := &fakeTailscale{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "state target mismatch") {
		t.Fatalf("expected state mismatch error, got %v", err)
	}
	if ts.created || h.userData != "" {
		t.Fatal("provider flow started before state mismatch was rejected")
	}
}

func TestCompleteProvisionUsesInjectedTime(t *testing.T) {
	fixed := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	st := state.State{}
	var saved state.State

	if err := completeProvision("state.json", &st, func(_ string, got state.State) error {
		saved = got
		return nil
	}, fixed); err != nil {
		t.Fatal(err)
	}
	if len(saved.Validations) != 1 || !saved.Validations[0].Time.Equal(fixed) {
		t.Fatalf("validations=%+v", saved.Validations)
	}
}
