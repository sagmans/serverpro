package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/state"
	"github.com/spf13/cobra"
)

type powerDeleteFakeProvider struct {
	readFakeProvider
	powerActions      []compute.PowerAction
	powerDeadline     time.Time
	deleted           bool
	deletedCount      int
	deleteDeadline    time.Time
	deleteDiagnostics compute.Diagnostics
}

func (p *powerDeleteFakeProvider) Power(ctx context.Context, request compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	p.powerActions = append(p.powerActions, request.Action)
	p.powerDeadline, _ = ctx.Deadline()
	return compute.ServerStatus{Power: "running"}, nil
}

func (p *powerDeleteFakeProvider) Delete(ctx context.Context, _ compute.DeleteServerRequest) compute.Diagnostics {
	p.deleted = true
	p.deletedCount++
	p.deleteDeadline, _ = ctx.Deadline()
	return p.deleteDiagnostics
}

func TestServerPowerCommandsUseProviderFacade(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  func(*app) *cobra.Command
		want compute.PowerAction
	}{
		{name: "start", cmd: func(a *app) *cobra.Command { return a.serverStartCmd() }, want: compute.PowerStart},
		{name: "stop", cmd: func(a *app) *cobra.Command { return a.serverStopCmd() }, want: compute.PowerStop},
		{name: "restart", cmd: func(a *app) *cobra.Command { return a.serverRestartCmd() }, want: compute.PowerRestart},
	} {
		t.Run(tc.name, func(t *testing.T) {
			createServerReadFixture(t)
			provider := &powerDeleteFakeProvider{}
			a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
			cmd := tc.cmd(a)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"webapp"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(provider.powerActions) != 1 || provider.powerActions[0] != tc.want {
				t.Fatalf("power actions=%v", provider.powerActions)
			}
		})
	}
}

func TestServerPowerUsesDefaultOperationDeadline(t *testing.T) {
	createServerReadFixture(t)
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverStartCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if provider.powerDeadline.IsZero() {
		t.Fatal("power operation reached provider without deadline")
	}
}

func TestServerDeleteUsesDefaultOperationDeadline(t *testing.T) {
	createServerReadFixture(t)
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if provider.deleteDeadline.IsZero() {
		t.Fatal("delete operation reached provider without deadline")
	}
}

func TestServerDeleteDryRunAndConfirmation(t *testing.T) {
	createServerReadFixture(t)
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", nonInteractive: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes required") || provider.deleted {
		t.Fatalf("expected confirmation error, got %v deleted=%t", err, provider.deleted)
	}

	a.dryRun = true
	cmd = a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if provider.deleted {
		t.Fatal("dry-run deleted provider resource")
	}
}

func TestServerDeleteDryRunListsProviderSpecificAccessPolicyID(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "digitalocean", key: "firewall_id"},
		{name: "vultr", key: "firewall_group_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			createServerReadFixture(t)
			stPath := config.ServerStatePath("demoapp", "webapp")
			st, err := state.Load(stPath)
			if err != nil {
				t.Fatal(err)
			}
			st.Compute.ProviderState = map[string]string{tc.key: "policy-1"}
			if err := state.Save(stPath, st); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", dryRun: true, jsonOut: true}
			cmd := a.serverDeleteCmd()
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"webapp"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var payload struct {
				AccessPolicyID string `json:"access_policy_id"`
			}
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatalf("dry-run output is not JSON: %v\n%s", err, out.String())
			}
			if payload.AccessPolicyID != "policy-1" {
				t.Fatalf("access policy id = %q", payload.AccessPolicyID)
			}
		})
	}
}

func TestServerDeleteDryRunListsTrackedExternalCleanup(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale = state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, NodeID: "node-1", AuthKeyID: "key-1", Tags: []string{"tag:serverpro-demoapp"}, PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}, PolicySSHUser: "deploy"}
	st.Cloudflare = state.CloudflareState{TunnelID: "tun-1", Name: "demoapp-webapp"}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", dryRun: true, jsonOut: true}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ExternalCleanup []struct {
			Type string   `json:"type"`
			ID   string   `json:"id"`
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		} `json:"external_cleanup"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, out.String())
	}
	got := map[string]bool{}
	for _, resource := range payload.ExternalCleanup {
		got[resource.Type+":"+resource.ID+":"+resource.Name+":"+strings.Join(resource.Tags, ",")] = true
	}
	for _, want := range []string{
		"tailscale_device:node-1::",
		"tailscale_auth_key:key-1::",
		"cloudflare_tunnel:tun-1:demoapp-webapp:",
		"tailscale_policy_tag_owners:::tag:serverpro-demoapp",
		"tailscale_ssh_rule:::tag:serverpro-demoapp",
	} {
		if !got[want] {
			t.Fatalf("dry-run missing %q in %+v\n%s", want, payload.ExternalCleanup, out.String())
		}
	}
}

func TestServerDeleteDryRunDoesNotListSharedPolicyAsDeleted(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale = state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}, PolicySSHUser: "deploy"}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{Project: "demoapp", Server: "api", Tailscale: state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, NodeID: "node-2", Tags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, stPath, siblingPath)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", dryRun: true, jsonOut: true}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ExternalCleanup []struct {
			Type string `json:"type"`
		} `json:"external_cleanup"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, out.String())
	}
	for _, resource := range payload.ExternalCleanup {
		if strings.HasPrefix(resource.Type, "tailscale_policy") || resource.Type == "tailscale_ssh_rule" {
			t.Fatalf("shared policy was falsely listed for deletion: %+v\n%s", payload.ExternalCleanup, out.String())
		}
	}
}

func TestServerDeleteConfirmationMentionsExternalResources(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale.NodeID = "node-1"
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	var out bytes.Buffer
	a := &app{stdin: strings.NewReader("no\n"), stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cancelled") || provider.deleted {
		t.Fatalf("expected cancelled delete without provider call, err=%v deleted=%t", err, provider.deleted)
	}
	if !strings.Contains(out.String(), "tracked external provider resources") {
		t.Fatalf("confirmation did not mention external resources:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"status": "planned"`) {
		t.Fatalf("plan preview not printed before confirmation:\n%s", out.String())
	}
}

func TestServerDeleteReportsProviderErrors(t *testing.T) {
	createServerReadFixture(t)
	provider := &powerDeleteFakeProvider{deleteDiagnostics: compute.Diagnostics{{Status: compute.Fail, Message: "delete failed"}}}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("expected provider delete error, got %v", err)
	}
}

func TestServerDeleteUsesProviderFacade(t *testing.T) {
	createServerReadFixture(t)
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !provider.deleted {
		t.Fatal("provider delete not called")
	}
}

func TestServerDeleteStopsBeforeComputeWhenTailnetIdentityMissing(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale.NodeID = "node-1"
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})

	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "tailnet identity missing") {
		t.Fatalf("expected tailnet identity error, got %v", err)
	}
	if provider.deleted {
		t.Fatal("compute deleted before structural Tailscale preflight")
	}
}

func TestServerDeleteStopsBeforeComputeForPartiallyAppliedPolicyOwnership(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale = state.TailscaleState{
		Tailnet:                cleanupTestTailnetSelector,
		TailnetID:              cleanupTestTailnetID,
		PolicyPendingTagOwners: []string{"tag:serverpro-demoapp"},
		PolicyPendingSSHRule:   true,
		PolicySSHTags:          []string{"tag:serverpro-demoapp"},
		PolicySSHUser:          "deploy",
	}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	tailscaleClient := &recordingCleanupTailscale{policyPresence: tailscale.ServerproPolicyChange{TagOwners: []string{"tag:serverpro-demoapp"}}}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider), services: serviceHooks{
		preflightTrackedExternalResources: func(ctx context.Context, cleanup *serverDeleteCleanup) error {
			return validateTrackedExternalResources(ctx, cleanup, serverCleanupClients{Tailscale: tailscaleClient})
		},
	}}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})

	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "partially applied") {
		t.Fatalf("expected partial ownership error, got %v", err)
	}
	if tailscaleClient.policyInspections != 1 || provider.deleted {
		t.Fatalf("partial ownership inspection=%d compute_deleted=%t", tailscaleClient.policyInspections, provider.deleted)
	}
}

func TestServerDeleteStopsBeforeComputeForSharedPolicyDrift(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale = state.TailscaleState{
		Tailnet:         cleanupTestTailnetSelector,
		TailnetID:       cleanupTestTailnetID,
		PolicyTagOwners: []string{"tag:serverpro-demoapp"},
		PolicySSHRule:   true,
		PolicySSHTags:   []string{"tag:serverpro-demoapp"},
		PolicySSHUser:   "deploy",
	}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{Project: "demoapp", Server: "api", Tailscale: state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, Tags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, stPath, siblingPath)

	provider := &powerDeleteFakeProvider{}
	tailscaleClient := &recordingCleanupTailscale{policyInspectErr: errors.New("ownership drift")}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider), services: serviceHooks{
		preflightTrackedExternalResources: func(ctx context.Context, cleanup *serverDeleteCleanup) error {
			return validateTrackedExternalResources(ctx, cleanup, serverCleanupClients{Tailscale: tailscaleClient})
		},
		deleteTrackedExternalResources: func(_ context.Context, cleanup serverDeleteCleanup) (state.State, error) {
			return cleanup.State, nil
		},
	}}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})

	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "ownership drift") {
		t.Fatalf("expected shared policy drift error, got %v", err)
	}
	if tailscaleClient.policyInspections != 1 || strings.Join(tailscaleClient.inspectedPolicyTags, ",") != "tag:serverpro-demoapp" || strings.Join(tailscaleClient.inspectedPolicySSHTags, ",") != "tag:serverpro-demoapp" || tailscaleClient.inspectedPolicyUser != "deploy" {
		t.Fatalf("shared policy inspection used wrong identity: %+v", tailscaleClient)
	}
	if provider.deleted {
		t.Fatal("compute deleted before shared policy drift preflight")
	}
}

func TestServerDeleteStopsBeforeComputeWhenTailnetPreflightFails(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale = state.TailscaleState{Tailnet: cleanupTestTailnetSelector, TailnetID: cleanupTestTailnetID, NodeID: "node-1"}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider), services: serviceHooks{
		preflightTrackedExternalResources: func(context.Context, *serverDeleteCleanup) error {
			return errors.New("tailnet identity mismatch")
		},
	}}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})

	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "tailnet identity mismatch") {
		t.Fatalf("expected tailnet preflight error, got %v", err)
	}
	if provider.deleted {
		t.Fatal("compute deleted before live Tailscale preflight")
	}
}

func TestServerDeleteRemovesLocalStateAndRegistry(t *testing.T) {
	createServerReadFixture(t)
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if state.Exists(config.Expand(config.ServerStatePath("demoapp", "webapp"))) {
		t.Fatal("state file still exists")
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("demoapp", "webapp"); ok {
		t.Fatal("registry entry still exists")
	}
}

func TestServerDeleteCleansExternalOnlyCheckpointWithoutComputeProvider(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(config.ServerConfigPath("demoapp", "webapp"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Project: "demoapp", Server: "webapp", ServerProvider: "unused", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := state.State{Project: "demoapp", Server: "webapp", Compute: state.ComputeState{Provider: "hetzner", Namespace: "demoapp", Server: "webapp", Name: cfg.Compute.Name}, Cloudflare: state.CloudflareState{TunnelID: "tun-1", Name: cfg.Cloudflare.Tunnel.Name}}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "webapp", StatePath: stPath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cleanupCalled := false
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", yes: true, services: serviceHooks{
		deleteTrackedExternalResources: func(context.Context, serverDeleteCleanup) (state.State, error) {
			cleanupCalled = true
			return st, nil
		},
	}}
	if err := a.deleteServerDestructive(context.Background(), "webapp", stPath, st); err != nil {
		t.Fatal(err)
	}
	if !cleanupCalled {
		t.Fatal("tracked external cleanup was not attempted")
	}
	if state.Exists(config.Expand(stPath)) {
		t.Fatal("external-only checkpoint state still exists")
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("demoapp", "webapp"); ok {
		t.Fatal("external-only checkpoint registry entry still exists")
	}
}

func TestServerDeletePreservesStateWhenExternalCleanupFails(t *testing.T) {
	createServerReadFixture(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(config.ServerConfigPath("demoapp", "webapp"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Project: "demoapp", Server: "webapp", ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Cloudflare = state.CloudflareState{TunnelID: "tun-1", Name: "demoapp-webapp"}
	st.Tailscale.NodeID = "node-1"
	st.Tailscale.PolicyTagOwners = []string{"tag:serverpro-demoapp"}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider), services: serviceHooks{
		preflightTrackedExternalResources: func(context.Context, *serverDeleteCleanup) error { return nil },
		deleteTrackedExternalResources: func(context.Context, serverDeleteCleanup) (state.State, error) {
			return state.State{}, errors.New("cleanup failed")
		},
	}}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("expected cleanup failure, got %v", err)
	}
	if !provider.deleted {
		t.Fatal("compute provider delete not attempted")
	}
	if !state.Exists(config.Expand(stPath)) {
		t.Fatal("state file was removed despite external cleanup failure")
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("demoapp", "webapp"); !ok {
		t.Fatal("registry entry was removed despite external cleanup failure")
	}
}

func providerRegistryForPower(t *testing.T, provider compute.Provider) *compute.Registry {
	t.Helper()
	registry := compute.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	return registry
}
