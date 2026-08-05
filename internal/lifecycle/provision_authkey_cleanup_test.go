package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunRetriesPersistedAuthKeyCleanupBeforeCreatingReplacement(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Project: cfg.Project, Server: cfg.Server, Compute: state.ComputeState{Name: cfg.Compute.Name}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}, Tailscale: state.TailscaleState{Tailnet: "-", TailnetID: "tailnet-1", AuthKeyID: "old-key"}}); err != nil {
		t.Fatal(err)
	}
	ts := &fakeTailscale{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ts.deletedKeyIDs, ","); got != "old-key,k1" {
		t.Fatalf("deleted auth key ids = %q", got)
	}
	if len(ts.calls) < 2 || strings.Join(ts.calls[:2], ",") != "tailnet-id,delete-key" {
		t.Fatalf("stale auth key cleanup did not follow identity validation: %v", ts.calls)
	}
}

func TestRunPreservesPersistedAuthKeyWhenRetryCleanupFails(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Project: cfg.Project, Server: cfg.Server, Compute: state.ComputeState{Name: cfg.Compute.Name}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}, Tailscale: state.TailscaleState{Tailnet: "-", TailnetID: "tailnet-1", AuthKeyID: "old-key"}}); err != nil {
		t.Fatal(err)
	}
	ts := &fakeTailscale{deleteErr: errors.New("cleanup failed")}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("expected stale auth key cleanup failure, got %v", err)
	}
	if ts.created {
		t.Fatal("replacement auth key was created before stale key cleanup")
	}
	saved, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.Tailscale.AuthKeyID != "old-key" {
		t.Fatalf("stale auth key id was lost: %+v", saved.Tailscale)
	}
}

// A 404 on the final best-effort auth-key delete means the one-off key already
// expired or was garbage-collected. Provisioning succeeded, so Run must report
// success and still mark the state completed with the key id cleared.
func TestRunCompletesWhenFinalAuthKeyCleanupReturnsNotFound(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	path := provisionStatePath(t)
	ts := &fakeTailscale{deleteErr: &httpjson.StatusError{Method: http.MethodDelete, Path: "/keys/k1", Status: "404 Not Found", StatusCode: http.StatusNotFound}}
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatalf("expected success on not-found auth key cleanup, got %v", err)
	}
	if st.Tailscale.AuthKeyID != "" {
		t.Fatalf("auth key id should be cleared after not-found cleanup: %+v", st.Tailscale)
	}
	saved, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatalf("state missing: %v", loadErr)
	}
	if saved.Tailscale.AuthKeyID != "" || len(saved.Validations) == 0 || !saved.Validations[len(saved.Validations)-1].Passed {
		t.Fatalf("provision not completed cleanly: %+v", saved)
	}
}

// A hard (non-404) failure on the final auth-key delete still must not fail a
// completed provision. Run reports success, the provision is marked completed,
// and the key id is retained in state so it can be retried later.
func TestRunCompletesAndRetainsKeyWhenFinalAuthKeyCleanupFails(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	path := provisionStatePath(t)
	ts := &fakeTailscale{deleteErr: &httpjson.StatusError{Method: http.MethodDelete, Path: "/keys/k1", Status: "500 Internal Server Error", StatusCode: http.StatusInternalServerError}}
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatalf("completed provision must not fail on best-effort cleanup error, got %v", err)
	}
	if !strings.Contains(strings.Join(ts.calls, ","), "delete-key") {
		t.Fatalf("expected auth key delete attempt, got %v", ts.calls)
	}
	saved, loadErr := state.Load(path)
	if loadErr != nil {
		t.Fatalf("state missing: %v", loadErr)
	}
	if saved.Tailscale.AuthKeyID != "k1" {
		t.Fatalf("failed cleanup should retain auth key id for retry: %+v", saved.Tailscale)
	}
	if len(saved.Validations) == 0 || !saved.Validations[len(saved.Validations)-1].Passed {
		t.Fatalf("provision should still be marked completed: %+v", saved)
	}
	_ = st
}
