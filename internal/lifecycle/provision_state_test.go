package lifecycle

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

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

func TestInitializeProvisionStateDoesNotRewriteCurrentState(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Project: "prod", Server: "web", Compute: state.ComputeState{Name: cfg.Compute.Name}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}}); err != nil {
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

func TestRunRejectsExistingStateForDifferentTarget(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Project: "other", Server: "web"}); err != nil {
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
