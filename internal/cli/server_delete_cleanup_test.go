package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

func TestDeleteTrackedExternalResourcesClearsAndSavesState(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	tailscaleClient := &recordingCleanupTailscale{devices: cleanupLiveDevices(), authKeys: cleanupLiveAuthKeys()}
	cloudflareClient := &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel()}

	updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient, Cloudflare: cloudflareClient})
	if err != nil {
		t.Fatal(err)
	}
	if tailscaleClient.deletedDevice != "node-1" || tailscaleClient.deletedAuthKey != "key-owned" || cloudflareClient.deletedTunnel != "tun-1" {
		t.Fatalf("cleanup calls tailscale=%+v cloudflare=%+v", tailscaleClient, cloudflareClient)
	}
	if updated.Tailscale.NodeID != "" || updated.Tailscale.AuthKeyID != "" || updated.Cloudflare.TunnelID != "" || len(updated.Tailscale.PolicyTagOwners) == 0 || !updated.Tailscale.PolicySSHRule {
		t.Fatalf("returned state lost global policy evidence: %+v", updated)
	}
	saved, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tailscale.NodeID != "" || saved.Tailscale.AuthKeyID != "" || saved.Cloudflare.TunnelID != "" || len(saved.Tailscale.PolicyTagOwners) == 0 || !saved.Tailscale.PolicySSHRule {
		t.Fatalf("saved state lost global policy evidence: %+v", saved)
	}
}

func TestAppDeleteTrackedExternalResourcesUsesInjectedClients(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Cloudflare = state.CloudflareState{}
	st.Tailscale.AuthKeyID = ""
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	client := &recordingCleanupTailscale{devices: cleanupLiveDevices()}
	a := &app{services: serviceHooks{cleanupClients: func(got serverDeleteCleanup) serverCleanupClients {
		if got.StatePath != stPath || got.State.Tailscale.NodeID != st.Tailscale.NodeID {
			t.Fatalf("bad cleanup args: %+v", got)
		}
		return serverCleanupClients{Tailscale: client}
	}}}
	updated, err := a.deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st})
	if err != nil {
		t.Fatal(err)
	}
	if client.deletedDevice != st.Tailscale.NodeID || updated.Tailscale.NodeID != "" {
		t.Fatalf("production cleanup not used: client=%+v state=%+v", client, updated.Tailscale)
	}
}

func TestNewServerCleanupClientsWiresProviderAdapters(t *testing.T) {
	clients := newServerCleanupClients(serverDeleteCleanup{})
	if clients.Tailscale == nil || clients.Cloudflare == nil {
		t.Fatalf("cleanup clients not wired: %+v", clients)
	}
}

func TestCleanupCloudflareAdapterMapsActiveConnections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1022,"message":"This tunnel has active connections."}]}`))
	}))
	t.Cleanup(server.Close)
	client := cleanupCloudflareAdapter{Client: cloudflare.NewWithHTTP("token", "account", server.URL, server.Client())}

	err := client.DeleteTunnel(context.Background(), "tun-1")
	if !errors.Is(err, errTunnelActiveConnections) {
		t.Fatalf("active connections were not normalized: %v", err)
	}
}

func TestDeleteTrackedExternalResourcesPreservesRemainingStateOnFailure(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	cloudflareClient := &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel(), deleteErr: errors.New("tunnel delete failed")}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: &recordingCleanupTailscale{devices: cleanupLiveDevices(), authKeys: cleanupLiveAuthKeys()}, Cloudflare: cloudflareClient})
	if err == nil || !strings.Contains(err.Error(), "tunnel delete failed") {
		t.Fatalf("expected tunnel failure, got %v", err)
	}
	saved, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tailscale.NodeID != "" {
		t.Fatalf("completed device cleanup was not checkpointed: %+v", saved.Tailscale)
	}
	if saved.Cloudflare.TunnelID != "tun-1" {
		t.Fatalf("failed tunnel cleanup should preserve tunnel state: %+v", saved.Cloudflare)
	}
}

func TestDeleteTrackedExternalResourcesBoundsTunnelActiveConnectionRetry(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	cloudflareClient := &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel(), deleteErr: errTunnelActiveConnections}
	waited := false

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{
		Cloudflare: cloudflareClient,
		wait: func(context.Context) error {
			waited = true
			return context.DeadlineExceeded
		},
	})
	if err == nil || !strings.Contains(err.Error(), "active connections") || !waited {
		t.Fatalf("expected bounded active connection retry error, got %v waited=%t", err, waited)
	}
}

func TestDeleteTrackedExternalResourcesRetainsTunnelsNotCreatedByServerpro(t *testing.T) {
	for _, provenance := range []state.CloudflareTunnelProvenance{"", state.CloudflareTunnelAdopted, state.CloudflareTunnelImported} {
		t.Run(string(provenance), func(t *testing.T) {
			createTestHome(t)
			stPath := config.ServerStatePath("demoapp", "webapp")
			st := state.State{
				Namespace:  "demoapp",
				Server:     "webapp",
				Cloudflare: state.CloudflareState{TunnelID: "tun-1", Name: "demoapp-webapp", Provenance: provenance},
			}
			if err := state.Save(stPath, st); err != nil {
				t.Fatal(err)
			}
			updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: config.ExampleServer("demoapp", "webapp"), StatePath: stPath, State: st}, serverCleanupClients{})
			if err != nil {
				t.Fatal(err)
			}
			if updated.Cloudflare.TunnelID != "tun-1" || updated.Cloudflare.Provenance != provenance {
				t.Fatalf("retained tunnel changed: %+v", updated.Cloudflare)
			}
		})
	}
}

func TestDeleteTrackedExternalResourcesRejectsMismatchedLiveTailscaleDevice(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Cloudflare = state.CloudflareState{}
	st.Tailscale.PolicyTagOwners = nil
	st.Tailscale.PolicySSHRule = false
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	tailscaleClient := &recordingCleanupTailscale{devices: []tailscale.Device{{ID: "node-1", Name: "other.tail.ts.net", Hostname: "other", Tags: st.Tailscale.Tags}}}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err == nil || !strings.Contains(err.Error(), "ownership") || tailscaleClient.deletedDevice != "" {
		t.Fatalf("expected ownership error without device delete, err=%v client=%+v", err, tailscaleClient)
	}
	saved, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tailscale.NodeID != "node-1" {
		t.Fatalf("mismatched live device should preserve state: %+v", saved.Tailscale)
	}
}

func TestDeleteTrackedExternalResourcesRejectsMismatchedLiveCloudflareTunnel(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Cloudflare.Tunnel.Enabled = true
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	cloudflareClient := &recordingCleanupCloudflare{tunnel: cloudflare.Tunnel{ID: "tun-1", Name: "other-webapp"}}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Cloudflare: cloudflareClient})
	if err == nil || !strings.Contains(err.Error(), "ownership") || cloudflareClient.deletedTunnel != "" {
		t.Fatalf("expected ownership error without tunnel delete, err=%v client=%+v", err, cloudflareClient)
	}
	saved, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloudflare.TunnelID != "tun-1" {
		t.Fatalf("mismatched live tunnel should preserve state: %+v", saved.Cloudflare)
	}
}

func TestDeleteTrackedExternalResourcesRejectsMismatchedTrackedAuthKey(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{AuthKeyID: "key-owned", Tags: []string{"tag:serverpro-demoapp"}}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	tailscaleClient := &recordingCleanupTailscale{authKeys: []tailscale.AuthKey{{ID: "key-owned", Description: "manual"}}}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err == nil || !strings.Contains(err.Error(), "ownership") || tailscaleClient.deletedAuthKey != "" {
		t.Fatalf("expected ownership error without auth key delete, err=%v client=%+v", err, tailscaleClient)
	}
	saved, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tailscale.AuthKeyID != "key-owned" {
		t.Fatalf("mismatched auth key should preserve state: %+v", saved.Tailscale)
	}
}

func TestDeleteTrackedExternalResourcesDeletesOnlyTrackedAuthKey(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{AuthKeyID: "key-owned"}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	tailscaleClient := &recordingCleanupTailscale{authKeys: cleanupLiveAuthKeys()}

	updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err != nil {
		t.Fatal(err)
	}
	if tailscaleClient.deletedAuthKey != "key-owned" {
		t.Fatalf("expected owned auth key delete only, client=%+v", tailscaleClient)
	}
	if updated.Tailscale.AuthKeyID != "" {
		t.Fatalf("auth key state not cleared: %+v", updated.Tailscale)
	}
}

func TestDeleteTrackedExternalResourcesLeavesTailnetGlobalPolicyUntouched(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale.NodeID = ""
	st.Tailscale.AuthKeyID = ""
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	client := &recordingCleanupTailscale{}

	updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: client})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tailscale.PolicyTagOwners) == 0 || !updated.Tailscale.PolicySSHRule {
		t.Fatalf("server cleanup rewrote global policy evidence: %+v", updated.Tailscale)
	}
}

func TestDeleteTrackedExternalResourcesLeavesSSHOnlyPolicyUntouched(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Access.Tailscale.Tags = []string{"tag:serverpro-new"}
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	tailscaleClient := &recordingCleanupTailscale{}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteTrackedExternalResourcesDoesNotInferGlobalPolicyOwnershipFromRegistry(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Access.Tailscale.Tags = []string{"tag:serverpro-demoapp", "tag:serverpro-unique"}
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{PolicyTagOwners: cfg.Access.Tailscale.Tags, PolicySSHRule: true, PolicySSHTags: cfg.Access.Tailscale.Tags}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{Namespace: "demoapp", Server: "api", Tailscale: state.TailscaleState{NodeID: "node-2", Tags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "webapp", StatePath: stPath})
		reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "api", StatePath: siblingPath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tailscaleClient := &recordingCleanupTailscale{}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteTrackedExternalResourcesKeepsSharedTailscalePolicy(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{Namespace: "demoapp", Server: "api", Tailscale: state.TailscaleState{NodeID: "node-2", Tags: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "webapp", StatePath: stPath})
		reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "api", StatePath: siblingPath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tailscaleClient := &recordingCleanupTailscale{}

	updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tailscale.PolicyTagOwners) == 0 || !updated.Tailscale.PolicySSHRule || len(updated.Tailscale.PolicySSHTags) == 0 {
		t.Fatalf("server cleanup should retain global policy evidence: %+v", updated.Tailscale)
	}
}

func cleanupTestState() state.State {
	return state.State{
		Namespace: "demoapp",
		Server:    "webapp",
		Compute:   state.ComputeState{Name: "demoapp-webapp"},
		Tailscale: state.TailscaleState{
			NodeID:          "node-1",
			AuthKeyID:       "key-owned",
			Name:            "demoapp-webapp",
			IPs:             []string{"100.64.0.1"},
			Tags:            []string{"tag:serverpro-demoapp"},
			PolicyTagOwners: []string{"tag:serverpro-demoapp"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demoapp"},
		},
		Cloudflare: state.CloudflareState{TunnelID: "tun-1", Name: "demoapp-webapp", Provenance: state.CloudflareTunnelCreated},
	}
}

func cleanupLiveDevices() []tailscale.Device {
	return []tailscale.Device{{ID: "node-1", NodeID: "node-1", Name: "demoapp-webapp.tail.ts.net", Hostname: "demoapp-webapp", Tags: []string{"tag:serverpro-demoapp"}}}
}

func cleanupLiveTunnel() cloudflare.Tunnel {
	return cloudflare.Tunnel{ID: "tun-1", Name: "demoapp-webapp"}
}

func cleanupLiveAuthKeys() []tailscale.AuthKey {
	return []tailscale.AuthKey{{ID: "key-owned", Description: "serverpro bootstrap", Capabilities: tailscale.AuthKeyCapabilities{Devices: tailscale.AuthKeyDeviceCapabilities{Create: tailscale.AuthKeyCreateCapabilities{Tags: []string{"tag:serverpro-demoapp"}}}}}}
}

type recordingCleanupTailscale struct {
	devices        []tailscale.Device
	authKeys       []tailscale.AuthKey
	deletedDevice  string
	deletedAuthKey string
}

func (c *recordingCleanupTailscale) Devices(context.Context) ([]tailscale.Device, error) {
	return c.devices, nil
}

func (c *recordingCleanupTailscale) AuthKeys(context.Context) ([]tailscale.AuthKey, error) {
	return c.authKeys, nil
}

func (c *recordingCleanupTailscale) DeleteDevice(_ context.Context, id string) error {
	c.deletedDevice = id
	return nil
}

func (c *recordingCleanupTailscale) DeleteAuthKey(_ context.Context, id string) error {
	c.deletedAuthKey = id
	return nil
}

type recordingCleanupCloudflare struct {
	tunnel        cloudflare.Tunnel
	deletedTunnel string
	deleteErr     error
}

func (c *recordingCleanupCloudflare) GetTunnel(context.Context, string) (cloudflare.Tunnel, error) {
	return c.tunnel, nil
}

func (c *recordingCleanupCloudflare) DeleteTunnel(_ context.Context, id string) error {
	c.deletedTunnel = id
	return c.deleteErr
}
