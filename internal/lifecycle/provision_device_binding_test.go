package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunPersistsDeviceBaselineAcrossWaitRetry(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	first := &fakeTailscale{baselineIDs: []string{"device-old", "node-old"}, waitErr: errors.New("wait failed")}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: first, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "wait failed") {
		t.Fatalf("expected wait failure, got %v", err)
	}
	saved, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Tailscale.DeviceBaselineCaptured || !reflect.DeepEqual(saved.Tailscale.PreexistingDeviceIDs, []string{"device-old", "node-old"}) || saved.Compute.ID == "" {
		t.Fatalf("baseline checkpoint missing: %+v", saved)
	}

	second := &fakeTailscale{waitDevice: tailscale.Device{ID: "device-new", NodeID: "node-new", Name: cfg.Compute.Name, Online: true}}
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: second, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.waitRequests) != 1 || !reflect.DeepEqual(second.waitRequests[0].ExcludedIDs, []string{"device-old", "node-old"}) || st.Tailscale.NodeID != "node-new" {
		t.Fatalf("retry lost baseline: requests=%+v state=%+v", second.waitRequests, st.Tailscale)
	}
	if strings.Contains(strings.Join(second.calls, ","), "snapshot-devices") {
		t.Fatalf("retry replaced persisted baseline: %v", second.calls)
	}
}

func TestRunReusesPersistedDeviceBindingOnRetry(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	first := &fakeTailscale{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: first, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{err: errors.New("bad sudo password")}}})
	if err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Fatalf("expected remote failure after device checkpoint, got %v", err)
	}
	saved, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tailscale.NodeID != "d1" {
		t.Fatalf("device binding not checkpointed: %+v", saved.Tailscale)
	}

	second := &fakeTailscale{waitDevice: tailscale.Device{ID: "device-d1", NodeID: "d1", Name: cfg.Compute.Name, Online: true}}
	if _, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: second, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}}); err != nil {
		t.Fatal(err)
	}
	if len(second.waitRequests) != 1 || second.waitRequests[0].DeviceID != "d1" {
		t.Fatalf("retry ignored persisted binding: %+v", second.waitRequests)
	}
}

func TestRunRejectsExistingComputeWithoutDeviceBaseline(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	st := state.State{Project: cfg.Project, Server: cfg.Server, Compute: state.ComputeState{ID: "2", Name: cfg.Compute.Name}, Cloudflare: state.CloudflareState{Name: cfg.Cloudflare.Tunnel.Name}}
	if err := state.Save(path, st); err != nil {
		t.Fatal(err)
	}
	ts := &fakeTailscale{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts"}, StatePath: path, Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "device baseline") {
		t.Fatalf("expected fail-closed baseline error, got %v", err)
	}
	if strings.Join(ts.calls, ",") != "tailnet-id" {
		t.Fatalf("mutable provider call began before binding check: %v", ts.calls)
	}
}
