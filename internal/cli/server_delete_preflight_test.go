package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

func TestValidateTrackedExternalResourcesReadsEveryOwnedResourceWithoutMutation(t *testing.T) {
	createTestHome(t)
	st := cleanupTestState()
	path := config.ServerStatePath("demoapp", "webapp")
	if err := state.Save(path, st); err != nil {
		t.Fatal(err)
	}
	cleanup := serverDeleteCleanup{Config: config.ExampleServer("demoapp", "webapp"), StatePath: path, State: st}
	tsClient := &recordingCleanupTailscale{devices: cleanupLiveDevices(), authKeys: cleanupLiveAuthKeys(), policyPresence: tailscale.ServerproPolicyChange{TagOwners: st.Tailscale.PolicyTagOwners, SSHRule: true}}
	cfClient := &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel()}

	if err := validateTrackedExternalResources(context.Background(), &cleanup, serverCleanupClients{Tailscale: tsClient, Cloudflare: cfClient}); err != nil {
		t.Fatal(err)
	}
	if tsClient.tailnetChecks != 1 || tsClient.deviceReads != 1 || tsClient.authKeyReads != 1 || tsClient.policyInspections != 1 || cfClient.tunnelReads != 1 {
		t.Fatalf("preflight reads tailscale=%+v cloudflare=%+v", tsClient, cfClient)
	}
	if tsClient.deletedDevice != "" || tsClient.deletedAuthKey != "" || len(tsClient.removedPolicyTags) != 0 || cfClient.deletedTunnel != "" {
		t.Fatalf("preflight mutated providers tailscale=%+v cloudflare=%+v", tsClient, cfClient)
	}
}

func TestValidateTrackedExternalResourcesRejectsPredictableCleanupFailures(t *testing.T) {
	tests := []struct {
		name       string
		tailscale  *recordingCleanupTailscale
		cloudflare *recordingCleanupCloudflare
		want       string
	}{
		{name: "device drift", tailscale: &recordingCleanupTailscale{devices: []tailscale.Device{{ID: "node-1", Name: "other", Tags: []string{"tag:serverpro-demoapp"}}}, authKeys: cleanupLiveAuthKeys()}, cloudflare: &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel()}, want: "ownership mismatch"},
		{name: "auth key drift", tailscale: &recordingCleanupTailscale{devices: cleanupLiveDevices(), authKeys: []tailscale.AuthKey{{ID: "key-owned", Description: "manual"}}}, cloudflare: &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel()}, want: "ownership mismatch"},
		{name: "policy drift", tailscale: &recordingCleanupTailscale{devices: cleanupLiveDevices(), authKeys: cleanupLiveAuthKeys(), policyInspectErr: errors.New("ownership drift")}, cloudflare: &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel()}, want: "ownership drift"},
		{name: "tunnel drift", tailscale: &recordingCleanupTailscale{devices: cleanupLiveDevices(), authKeys: cleanupLiveAuthKeys()}, cloudflare: &recordingCleanupCloudflare{tunnel: cleanupLiveTunnel(), getErr: errors.New("tunnel read denied")}, want: "tunnel read denied"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			createTestHome(t)
			st := cleanupTestState()
			path := config.ServerStatePath("demoapp", "webapp")
			if err := state.Save(path, st); err != nil {
				t.Fatal(err)
			}
			cleanup := serverDeleteCleanup{Config: config.ExampleServer("demoapp", "webapp"), StatePath: path, State: st}

			err := validateTrackedExternalResources(context.Background(), &cleanup, serverCleanupClients{Tailscale: test.tailscale, Cloudflare: test.cloudflare})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if test.tailscale.deletedDevice != "" || test.tailscale.deletedAuthKey != "" || len(test.tailscale.removedPolicyTags) != 0 || test.cloudflare.deletedTunnel != "" {
				t.Fatalf("failed preflight mutated providers tailscale=%+v cloudflare=%+v", test.tailscale, test.cloudflare)
			}
		})
	}
}

func TestValidateTrackedExternalResourcesReconcilesPendingPolicy(t *testing.T) {
	createTestHome(t)
	path := config.ServerStatePath("demoapp", "webapp")
	st := state.State{Project: "demoapp", Server: "webapp", Tailscale: state.TailscaleState{
		Tailnet:                cleanupTestTailnetSelector,
		TailnetID:              cleanupTestTailnetID,
		PolicyPendingTagOwners: []string{"tag:serverpro-demoapp"},
		PolicyPendingSSHRule:   true,
		PolicySSHTags:          []string{"tag:serverpro-demoapp"},
		PolicySSHUser:          "deploy",
	}}
	if err := state.Save(path, st); err != nil {
		t.Fatal(err)
	}
	cleanup := serverDeleteCleanup{Config: config.ExampleServer("demoapp", "webapp"), StatePath: path, State: st}
	client := &recordingCleanupTailscale{policyPresence: tailscale.ServerproPolicyChange{TagOwners: []string{"tag:serverpro-demoapp"}, SSHRule: true}}

	if err := validateTrackedExternalResources(context.Background(), &cleanup, serverCleanupClients{Tailscale: client}); err != nil {
		t.Fatal(err)
	}
	if tailscalePolicyOwnershipPending(cleanup.State.Tailscale) || !cleanup.State.Tailscale.PolicySSHRule || len(cleanup.State.Tailscale.PolicyTagOwners) != 1 {
		t.Fatalf("pending ownership not reconciled: %+v", cleanup.State.Tailscale)
	}
}
