package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

// Regression: a second server in the same namespace reuses the existing SSH rule
// idempotently and therefore records PolicySSHRule=false with no PolicySSHTags.
// Deleting the first server (which created the rule) must still detect the
// sibling as a reference and keep the shared SSH rule, or the sibling loses its
// only admin path.
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
	sibling := state.State{Project: "demoapp", Server: "api", Tailscale: state.TailscaleState{NodeID: "node-2", Tags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, stPath, siblingPath)
	tailscaleClient := &recordingCleanupTailscale{}

	updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err != nil {
		t.Fatal(err)
	}
	if len(tailscaleClient.removedPolicyTags) != 0 || len(tailscaleClient.removedPolicySSHTags) != 0 {
		t.Fatalf("shared policy reused by sibling must not be removed: %+v", tailscaleClient)
	}
	if len(updated.Tailscale.PolicyTagOwners) != 0 || updated.Tailscale.PolicySSHRule || len(updated.Tailscale.PolicySSHTags) != 0 {
		t.Fatalf("deleted server state should clear its own policy ownership: %+v", updated.Tailscale)
	}
}

// Regression: when a sibling state is unreadable, cleanup must stop before
// clearing the only durable policy owner because no safe transfer target exists.
func TestDeleteStopsWhenSharedPolicyOwnerCannotBeTransferred(t *testing.T) {
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
	if err := state.Save(siblingPath, state.State{Project: "demoapp", Server: "api"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Expand(siblingPath), []byte("{ not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, stPath, siblingPath)
	tailscaleClient := &recordingCleanupTailscale{}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: stPath, State: st}, serverCleanupClients{Tailscale: tailscaleClient})
	if err == nil || !strings.Contains(err.Error(), "transfer") {
		t.Fatalf("expected ownership transfer failure, got %v", err)
	}
	if len(tailscaleClient.removedPolicyTags) != 0 || len(tailscaleClient.removedPolicySSHTags) != 0 {
		t.Fatalf("unreadable sibling must force fail-closed policy retention: %+v", tailscaleClient)
	}
	saved, loadErr := state.Load(stPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(saved.Tailscale.PolicyTagOwners) == 0 || !saved.Tailscale.PolicySSHRule {
		t.Fatalf("failed transfer cleared policy ownership: %+v", saved.Tailscale)
	}
}

func TestDeleteTransfersSharedPolicyOwnershipToFinalSibling(t *testing.T) {
	createTestHome(t)
	ownerConfig := config.ExampleServer("demoapp", "webapp")
	ownerPath := config.ServerStatePath("demoapp", "webapp")
	owner := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			PolicyTagOwners: []string{"tag:serverpro-demoapp"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demoapp"},
		},
	}
	if err := state.Save(ownerPath, owner); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{
		Project:   "demoapp",
		Server:    "api",
		Tailscale: state.TailscaleState{Tags: []string{"tag:serverpro-demoapp"}},
	}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, ownerPath, siblingPath)

	if _, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: ownerConfig, StatePath: ownerPath, State: owner}, serverCleanupClients{Tailscale: &recordingCleanupTailscale{}}); err != nil {
		t.Fatal(err)
	}
	transferred, err := state.Load(siblingPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(transferred.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-demoapp" || !transferred.Tailscale.PolicySSHRule || strings.Join(transferred.Tailscale.PolicySSHTags, ",") != "tag:serverpro-demoapp" {
		t.Fatalf("shared policy ownership was not transferred: %+v", transferred.Tailscale)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Remove("demoapp", "webapp")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	finalClient := &recordingCleanupTailscale{}
	if _, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: config.ExampleServer("demoapp", "api"), StatePath: siblingPath, State: transferred}, serverCleanupClients{Tailscale: finalClient}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(finalClient.removedPolicyTags, ",") != "tag:serverpro-demoapp" || strings.Join(finalClient.removedPolicySSHTags, ",") != "tag:serverpro-demoapp" {
		t.Fatalf("final sibling did not remove transferred policy: %+v", finalClient)
	}
}

func registerCleanupSiblings(t *testing.T, statePaths ...string) {
	t.Helper()
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		for _, p := range statePaths {
			st, err := state.Load(p)
			if err != nil {
				// Corrupt sibling: still register it so the cleanup scan encounters it.
				reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "api", StatePath: p})
				continue
			}
			reg.Upsert(state.RegistryEntry{Project: st.Project, Server: st.Server, StatePath: p})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
