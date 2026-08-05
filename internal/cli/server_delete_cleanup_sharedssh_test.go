package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

// Legacy regression: older sibling state can omit SSH-rule identity after
// idempotently reusing the rule. Cleanup must retain and transfer ownership
// rather than risk removing that sibling's only admin path.
func TestDeleteKeepsSharedSSHRuleWhenSiblingReusedItWithoutFlag(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := cleanupTestState()
	st.Tailscale = state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}, PolicySSHUser: "deploy"}
	st.Cloudflare = state.CloudflareState{}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	// This fixture deliberately models state written before exact SSH-rule identity tracking.
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{Project: "demoapp", Server: "api", Tailscale: state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, NodeID: "node-2", Tags: []string{"tag:serverpro-demoapp"}}}
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
	st.Tailscale = state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}, PolicySSHUser: "deploy"}
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
			Tailnet:         cleanupTestTailnetSelector,
			TailnetID:       cleanupTestTailnetID,
			PolicyTagOwners: []string{"tag:serverpro-demoapp"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demoapp"},
			PolicySSHUser:   "deploy",
		},
	}
	if err := state.Save(ownerPath, owner); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{
		Project: "demoapp",
		Server:  "api",
		Tailscale: state.TailscaleState{
			Tailnet:   cleanupTestTailnetSelector,
			TailnetID: cleanupTestTailnetID,
			Tags:      []string{"tag:serverpro-demoapp"},
		},
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
	if strings.Join(transferred.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-demoapp" || !transferred.Tailscale.PolicySSHRule || strings.Join(transferred.Tailscale.PolicySSHTags, ",") != "tag:serverpro-demoapp" || transferred.Tailscale.PolicySSHUser != "deploy" {
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
	if strings.Join(finalClient.removedPolicyTags, ",") != "tag:serverpro-demoapp" || strings.Join(finalClient.removedPolicySSHTags, ",") != "tag:serverpro-demoapp" || finalClient.removedPolicyUser != "deploy" {
		t.Fatalf("final sibling did not remove transferred policy: %+v", finalClient)
	}
}

func TestDeleteTransfersPolicyBeforeSiblingDeviceEnrollment(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	ownerPath := config.ServerStatePath("demoapp", "webapp")
	owner := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			Tailnet:         cleanupTestTailnetSelector,
			TailnetID:       cleanupTestTailnetID,
			PolicyTagOwners: []string{"tag:serverpro-demoapp"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demoapp"},
			PolicySSHUser:   "deploy",
		},
	}
	if err := state.Save(ownerPath, owner); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	// Policy identity is checkpointed before device enrollment populates Tags.
	sibling := state.State{
		Project: "demoapp",
		Server:  "api",
		Tailscale: state.TailscaleState{
			Tailnet:       cleanupTestTailnetSelector,
			TailnetID:     cleanupTestTailnetID,
			PolicySSHTags: []string{"tag:serverpro-demoapp"},
			PolicySSHUser: "deploy",
		},
	}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, ownerPath, siblingPath)
	client := &recordingCleanupTailscale{}

	if _, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: ownerPath, State: owner}, serverCleanupClients{Tailscale: client}); err != nil {
		t.Fatal(err)
	}
	if len(client.removedPolicyTags) != 0 || len(client.removedPolicySSHTags) != 0 {
		t.Fatalf("pre-enrollment sibling lost shared policy: %+v", client)
	}
	transferred, err := state.Load(siblingPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(transferred.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-demoapp" || !transferred.Tailscale.PolicySSHRule {
		t.Fatalf("pre-enrollment ownership not transferred: %+v", transferred.Tailscale)
	}
}

func TestDeleteDoesNotSharePolicyAcrossTailnets(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	ownerPath := config.ServerStatePath("demoapp", "webapp")
	owner := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			Tailnet:         cleanupTestTailnetSelector,
			TailnetID:       cleanupTestTailnetID,
			PolicyTagOwners: []string{"tag:serverpro-demoapp"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demoapp"},
			PolicySSHUser:   "deploy",
		},
	}
	if err := state.Save(ownerPath, owner); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{
		Project: "demoapp",
		Server:  "api",
		Tailscale: state.TailscaleState{
			Tailnet:       "other.example.ts.net",
			TailnetID:     "tailnet-2",
			Tags:          []string{"tag:serverpro-demoapp"},
			PolicySSHRule: true,
			PolicySSHTags: []string{"tag:serverpro-demoapp"},
			PolicySSHUser: "deploy",
		},
	}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, ownerPath, siblingPath)
	client := &recordingCleanupTailscale{}

	if _, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: ownerPath, State: owner}, serverCleanupClients{Tailscale: client}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(client.removedPolicyTags, ",") != "tag:serverpro-demoapp" || strings.Join(client.removedPolicySSHTags, ",") != "tag:serverpro-demoapp" {
		t.Fatalf("different-tailnet sibling retained owner policy: %+v", client)
	}
}

func TestDeleteStopsForSiblingWithUnknownTailnetIdentity(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	ownerPath := config.ServerStatePath("demoapp", "webapp")
	owner := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			Tailnet:         cleanupTestTailnetSelector,
			TailnetID:       cleanupTestTailnetID,
			PolicyTagOwners: []string{"tag:serverpro-demoapp"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demoapp"},
			PolicySSHUser:   "deploy",
		},
	}
	if err := state.Save(ownerPath, owner); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	// This fixture deliberately models state written before tailnet identity tracking.
	sibling := state.State{Project: "demoapp", Server: "api", Tailscale: state.TailscaleState{Tags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, ownerPath, siblingPath)
	client := &recordingCleanupTailscale{}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: ownerPath, State: owner}, serverCleanupClients{Tailscale: client})
	if err == nil || !strings.Contains(err.Error(), "transfer") {
		t.Fatalf("expected unknown-tailnet transfer failure, got %v", err)
	}
	if len(client.removedPolicyTags) != 0 || len(client.removedPolicySSHTags) != 0 {
		t.Fatalf("unknown-tailnet sibling allowed policy mutation: %+v", client)
	}
}

func TestDeleteStopsWhenOnlySharedPolicySuccessorIsPending(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	ownerPath := config.ServerStatePath("demoapp", "webapp")
	owner := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			Tailnet:         cleanupTestTailnetSelector,
			TailnetID:       cleanupTestTailnetID,
			PolicyTagOwners: []string{"tag:serverpro-demoapp"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demoapp"},
			PolicySSHUser:   "deploy",
		},
	}
	if err := state.Save(ownerPath, owner); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{
		Project: "demoapp",
		Server:  "api",
		Tailscale: state.TailscaleState{
			Tailnet:                cleanupTestTailnetSelector,
			TailnetID:              cleanupTestTailnetID,
			PolicyPendingTagOwners: []string{"tag:serverpro-demoapp"},
			PolicyPendingSSHRule:   true,
			PolicySSHTags:          []string{"tag:serverpro-demoapp"},
			PolicySSHUser:          "deploy",
		},
	}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, ownerPath, siblingPath)
	client := &recordingCleanupTailscale{}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: ownerPath, State: owner}, serverCleanupClients{Tailscale: client})
	if err == nil || !strings.Contains(err.Error(), "transfer") {
		t.Fatalf("expected pending successor transfer error, got %v", err)
	}
	if len(client.removedPolicyTags) != 0 || len(client.removedPolicySSHTags) != 0 {
		t.Fatalf("pending successor allowed policy mutation: %+v", client)
	}
}

func TestDeleteUsesTrackedSSHUserAfterConfigDrift(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Admin.Username = "renamed"
	statePath := config.ServerStatePath("demoapp", "webapp")
	st := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			Tailnet:       cleanupTestTailnetSelector,
			TailnetID:     cleanupTestTailnetID,
			PolicySSHRule: true,
			PolicySSHTags: []string{"tag:serverpro-demoapp"},
			PolicySSHUser: "deploy",
		},
	}
	if err := state.Save(statePath, st); err != nil {
		t.Fatal(err)
	}
	client := &recordingCleanupTailscale{}

	updated, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: statePath, State: st}, serverCleanupClients{Tailscale: client})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(client.removedPolicySSHTags, ",") != "tag:serverpro-demoapp" || client.removedPolicyUser != "deploy" {
		t.Fatalf("cleanup used drifted config identity: %+v", client)
	}
	if updated.Tailscale.PolicySSHRule || updated.Tailscale.PolicySSHUser != "" {
		t.Fatalf("removed policy identity remained tracked: %+v", updated.Tailscale)
	}
}

func TestDeleteRemovesSSHRuleWhenSameTagsUseDifferentAdmin(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	ownerPath := config.ServerStatePath("demoapp", "webapp")
	owner := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			Tailnet:       cleanupTestTailnetSelector,
			TailnetID:     cleanupTestTailnetID,
			PolicySSHRule: true,
			PolicySSHTags: []string{"tag:serverpro-demoapp"},
			PolicySSHUser: "deploy",
		},
	}
	if err := state.Save(ownerPath, owner); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{
		Project: "demoapp",
		Server:  "api",
		Tailscale: state.TailscaleState{
			Tailnet:       cleanupTestTailnetSelector,
			TailnetID:     cleanupTestTailnetID,
			Tags:          []string{"tag:serverpro-demoapp"},
			PolicySSHTags: []string{"tag:serverpro-demoapp"},
			PolicySSHUser: "operator",
		},
	}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, ownerPath, siblingPath)
	client := &recordingCleanupTailscale{}

	if _, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: ownerPath, State: owner}, serverCleanupClients{Tailscale: client}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(client.removedPolicySSHTags, ",") != "tag:serverpro-demoapp" || client.removedPolicyUser != "deploy" {
		t.Fatalf("different-user sibling incorrectly retained rule: %+v", client)
	}
}

func TestDeleteFailsClosedWhenTrackedSSHUserMissing(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	statePath := config.ServerStatePath("demoapp", "webapp")
	st := state.State{
		Project: "demoapp",
		Server:  "webapp",
		Tailscale: state.TailscaleState{
			Tailnet:       cleanupTestTailnetSelector,
			TailnetID:     cleanupTestTailnetID,
			PolicySSHRule: true,
			PolicySSHTags: []string{"tag:serverpro-demoapp"},
		},
	}
	if err := state.Save(statePath, st); err != nil {
		t.Fatal(err)
	}
	client := &recordingCleanupTailscale{}

	_, err := deleteTrackedExternalResources(context.Background(), serverDeleteCleanup{Config: cfg, StatePath: statePath, State: st}, serverCleanupClients{Tailscale: client})
	if err == nil || !strings.Contains(err.Error(), "SSH policy identity") {
		t.Fatalf("expected incomplete identity error, got %v", err)
	}
	if len(client.removedPolicySSHTags) != 0 {
		t.Fatalf("legacy identity must fail before policy mutation: %+v", client)
	}
	persisted, loadErr := state.Load(statePath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !persisted.Tailscale.PolicySSHRule {
		t.Fatalf("failed cleanup cleared legacy ownership: %+v", persisted.Tailscale)
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
