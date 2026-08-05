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

func TestRunDeletesCheckpointedAuthKeyBeforeCreatingReplacement(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "prod", Server: "web", Tailscale: state.TailscaleState{AuthKeyID: "old-key"}}); err != nil {
		t.Fatal(err)
	}
	ts := &fakeTailscale{}
	if _, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}}); err != nil {
		t.Fatal(err)
	}
	if len(ts.deletedKeyIDs) != 2 || ts.deletedKeyIDs[0] != "old-key" || ts.deletedKeyIDs[1] != "k1" {
		t.Fatalf("checkpointed auth key was not replaced safely: calls=%v deleted=%v", ts.calls, ts.deletedKeyIDs)
	}
	if got := strings.Join(ts.calls, ","); !strings.Contains(got, "delete-key,create-key") {
		t.Fatalf("replacement key created before stale key cleanup: %s", got)
	}
}

func TestRunKeepsCheckpointedAuthKeyWhileExistingComputeBoots(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	st := state.State{
		Namespace: "prod", Server: "web",
		Compute:   state.ComputeState{Provider: "hetzner", ID: "2", Name: cfg.Compute.Name},
		Tailscale: state.TailscaleState{AuthKeyID: "old-key"},
	}
	if err := state.Save(path, st); err != nil {
		t.Fatal(err)
	}
	ts := &fakeTailscale{}
	if _, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}}); err != nil {
		t.Fatal(err)
	}
	if ts.created || len(ts.deletedKeyIDs) != 1 || ts.deletedKeyIDs[0] != "old-key" {
		t.Fatalf("existing compute should retain its bootstrap key until device readiness: created=%t deleted=%v calls=%v", ts.created, ts.deletedKeyIDs, ts.calls)
	}
}

func TestRunStopsWhenCheckpointedAuthKeyCleanupFails(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	if err := state.Save(path, state.State{Namespace: "prod", Server: "web", Tailscale: state.TailscaleState{AuthKeyID: "old-key"}}); err != nil {
		t.Fatal(err)
	}
	ts := &fakeTailscale{deleteErr: context.Canceled}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	var provisionErr *ProvisionError
	if !errors.As(err, &provisionErr) || provisionErr.Phase != ProvisionPhaseTailscaleAuthKey || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected typed stale-key cleanup failure, got %T %v", err, err)
	}
	if ts.created {
		t.Fatal("replacement auth key created after stale-key cleanup failure")
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
