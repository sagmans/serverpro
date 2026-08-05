package cli

import (
	"context"
	"os"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

// Server deletion never owns tailnet-global policy, regardless of what sibling
// state records; explicit tailnet reconciliation owns that decision.
func TestDeleteKeepsSharedSSHRuleWhenSiblingReusedItWithoutFlag(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	// Sibling reused the rule: same tag, but PolicySSHRule=false and no PolicySSHTags.
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{Namespace: "demoapp", Server: "api", Tailscale: state.TailscaleState{NodeID: "node-2", Tags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, stPath, siblingPath)
	tailscaleClient := &recordingCleanupTailscale{}

	updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tailscale.PolicyTagOwners) == 0 || !updated.Tailscale.PolicySSHRule || len(updated.Tailscale.PolicySSHTags) == 0 {
		t.Fatalf("server cleanup should retain global policy evidence: %+v", updated.Tailscale)
	}
}

// Unreadable siblings cannot affect server-scoped cleanup because global policy
// is reconciled only by the explicit tailnet operation.
func TestDeleteKeepsSharedPolicyWhenSiblingStateUnreadable(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	// Save a valid sibling first (creates the directory), then corrupt it so
	// state.Load fails for that registry entry.
	siblingPath := config.ServerStatePath("demoapp", "api")
	if err := state.Save(siblingPath, state.State{Namespace: "demoapp", Server: "api"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Expand(siblingPath), []byte("{ not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, stPath, siblingPath)
	tailscaleClient := &recordingCleanupTailscale{}

	if _, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient}); err != nil {
		t.Fatal(err)
	}
}

func registerCleanupSiblings(t *testing.T, statePaths ...string) {
	t.Helper()
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		for _, p := range statePaths {
			st, err := state.Load(p)
			if err != nil {
				// Corrupt sibling: still register it so the cleanup scan encounters it.
				reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "api", StatePath: p})
				continue
			}
			reg.Upsert(state.RegistryEntry{Namespace: st.Namespace, Server: st.Server, StatePath: p})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
