package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func createNamespaceDeleteFixture(t *testing.T, servers []string) *powerDeleteFakeProvider {
	t.Helper()
	createTestHome(t)
	provider := &powerDeleteFakeProvider{}
	reg := state.NewRegistry()
	for _, srv := range servers {
		cfg := config.ExampleServer("demoapp", srv)
		cfg.Admin.Username = "operator"
		cfg.Cloudflare.AccountID = "acc"
		if err := config.Save(config.ServerConfigPath("demoapp", srv), cfg); err != nil {
			t.Fatal(err)
		}
		if err := credentials.Save(cfg, credentials.Set{Namespace: "demoapp", Server: srv, ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
			t.Fatal(err)
		}
		st := state.State{
			Namespace: "demoapp",
			Server:    srv,
			Compute:   state.ComputeState{Provider: "hetzner", Account: "prod", Namespace: "demoapp", Server: srv, ID: srv + "-id", Name: "demoapp-" + srv, Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"},
		}
		if err := state.Save(config.ServerStatePath("demoapp", srv), st); err != nil {
			t.Fatal(err)
		}
		reg.Upsert(state.RegistryEntry{
			Namespace:       "demoapp",
			Server:          srv,
			StatePath:       config.ServerStatePath("demoapp", srv),
			ConfigPath:      config.ServerConfigPath("demoapp", srv),
			CredentialsPath: config.ServerCredentialsPath("demoapp", srv),
		})
	}
	reg.UpsertNamespace("demoapp")
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestNamespaceDeleteDryRunShowsPlanWithoutMutations(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web", "api"})
	stPath := config.ServerStatePath("demoapp", "web")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Cloudflare = state.CloudflareState{TunnelID: "tun-web", Name: "demoapp-web", Provenance: state.CloudflareTunnelCreated}
	st.Tailscale.NodeID = "node-web"
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard, dryRun: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.namespaceDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if provider.deleted {
		t.Fatal("dry-run deleted provider resource")
	}

	var row namespaceDeleteRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if row.Status != "planned" || !row.DryRun || row.Namespace != "demoapp" || row.ServerCount != 2 {
		t.Fatalf("bad plan row: %+v", row)
	}
	if len(row.Servers) != 2 {
		t.Fatalf("expected 2 server plans, got %d", len(row.Servers))
	}
	if len(row.LocalCleanup) != 2 {
		t.Fatalf("expected 2 local cleanup paths, got %d", len(row.LocalCleanup))
	}

	var webPlan namespaceDeleteServerPlan
	for _, s := range row.Servers {
		if s.Server == "web" {
			webPlan = s
		}
	}
	if webPlan.Server != "web" || webPlan.Provider != "hetzner" || webPlan.ComputeServer != "demoapp-web" {
		t.Fatalf("bad web plan: %+v", webPlan)
	}
	foundTunnel := false
	for _, r := range webPlan.ExternalCleanup {
		if r.Type == deleteResourceCloudflareTunnel && r.ID == "tun-web" {
			foundTunnel = true
		}
	}
	if !foundTunnel {
		t.Fatalf("missing cloudflare tunnel in external cleanup: %+v", webPlan.ExternalCleanup)
	}

	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("demoapp", "web"); !ok {
		t.Fatal("registry entry removed during dry-run")
	}
	if _, err := os.Stat(config.ServerStatePath("demoapp", "web")); err != nil {
		t.Fatalf("state removed during dry-run: %v", err)
	}
}

func TestNamespaceDeleteDryRunPlansMissingServerStateAsLocalOnly(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web", "stale"})
	if err := os.Remove(config.ServerStatePath("demoapp", "stale")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard, dryRun: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.namespaceDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if provider.deleted {
		t.Fatal("dry-run deleted provider resource")
	}

	var row namespaceDeleteRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if row.ServerCount != 2 || len(row.Servers) != 2 {
		t.Fatalf("expected stale server in plan: %+v", row)
	}

	var stalePlan namespaceDeleteServerPlan
	for _, plan := range row.Servers {
		if plan.Server == "stale" {
			stalePlan = plan
		}
	}
	if stalePlan.Server != "stale" {
		t.Fatalf("missing stale server plan: %+v", row.Servers)
	}
	if stalePlan.Provider != "" || len(stalePlan.ExternalCleanup) != 0 {
		t.Fatalf("stale state should not plan provider cleanup: %+v", stalePlan)
	}
	if stalePlan.StatePath != config.Expand(config.ServerStatePath("demoapp", "stale")) || stalePlan.ConfigPath == "" || stalePlan.CredentialsPath == "" {
		t.Fatalf("stale plan missing local paths: %+v", stalePlan)
	}
}

func TestLoadNamespaceDeleteServerAuthoritiesCapturesStatePresence(t *testing.T) {
	createNamespaceDeleteFixture(t, []string{"web", "stale"})
	if err := os.Remove(config.ServerStatePath("demoapp", "stale")); err != nil {
		t.Fatal(err)
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	authorities, err := loadNamespaceDeleteServerAuthorities(reg.List("demoapp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(authorities) != 2 {
		t.Fatalf("authority count = %d", len(authorities))
	}
	presence := make(map[string]bool, len(authorities))
	for _, authority := range authorities {
		presence[authority.Entry.Server] = authority.StateExists
		if authority.StateExists && authority.State.Server != authority.Entry.Server {
			t.Fatalf("state identity = %q, want %q", authority.State.Server, authority.Entry.Server)
		}
	}
	if !presence["web"] || presence["stale"] {
		t.Fatalf("state presence = %#v", presence)
	}
}

func TestNamespaceDeleteRemovesStaleRegistryEntryBeforeLaterFailure(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"aa", "web"})
	if err := os.Remove(config.ServerStatePath("demoapp", "aa")); err != nil {
		t.Fatal(err)
	}
	provider.deleteDiagnostics = compute.Diagnostics{{Status: compute.Fail, Message: "provider delete failed"}}

	var out bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.namespaceDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"demoapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "provider delete failed") {
		t.Fatalf("expected provider error, got %v", err)
	}
	if provider.deletedCount != 1 {
		t.Fatalf("expected one provider delete attempt, got %d", provider.deletedCount)
	}

	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("demoapp", "aa"); ok {
		t.Fatal("stale server still in registry")
	}
	if _, ok := reg.Find("demoapp", "web"); !ok {
		t.Fatal("remaining server removed after provider failure")
	}
	if !namespaceExists(reg, "demoapp") {
		t.Fatal("namespace removed after provider failure")
	}
}

func TestRemoveNamespaceServerRegistryEntryRetainsReplacement(t *testing.T) {
	createNamespaceDeleteFixture(t, []string{"stale"})
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	approved, exists := reg.Find("demoapp", "stale")
	if !exists {
		t.Fatal("approved stale entry missing")
	}
	replacementStatePath := filepath.Join(t.TempDir(), "replacement.json")
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "stale", StatePath: replacementStatePath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := removeNamespaceServerRegistryEntry(approved); err == nil || !strings.Contains(err.Error(), "authority changed") {
		t.Fatalf("replacement cleanup error = %v", err)
	}
	reg, err = state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	current, exists := reg.Find("demoapp", "stale")
	if !exists || config.Expand(current.StatePath) != replacementStatePath {
		t.Fatalf("replacement stale entry lost: exists=%t entry=%+v", exists, current)
	}
}

func TestNamespaceDeleteRejectsReplacementWhileAwaitingNamespaceLock(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"stale"})
	approvedStatePath := config.ServerStatePath("demoapp", "stale")
	if err := os.Remove(approvedStatePath); err != nil {
		t.Fatal(err)
	}
	unlock, err := state.LockNamespaceOperation(context.Background(), config.RegistryPath(), "demoapp")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{stdout: io.Discard, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	done := make(chan error, 1)
	go func() { done <- a.runNamespaceDelete(context.Background(), "demoapp") }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("namespace delete bypassed namespace lock: %v", err)
	case <-time.After(serverOperationLockProbe):
	}

	replacementStatePath := filepath.Join(t.TempDir(), "replacement.json")
	replacement := state.State{Namespace: "demoapp", Server: "stale", Compute: state.ComputeState{Provider: "hetzner", ID: "replacement-id", Name: "replacement"}}
	if err := state.Save(replacementStatePath, replacement); err != nil {
		unlock()
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "stale", StatePath: replacementStatePath})
		return nil
	}); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "namespace destructive authority changed") {
			t.Fatalf("replacement authority error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("namespace delete did not finish after lock release")
	}
	if provider.deleted {
		t.Fatal("replacement reached provider delete")
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	entry, exists := reg.Find("demoapp", "stale")
	if !exists || config.Expand(entry.StatePath) != replacementStatePath {
		t.Fatalf("replacement registry authority lost: exists=%t entry=%+v", exists, entry)
	}
	if exists, err := state.Exists(replacementStatePath); err != nil || !exists {
		t.Fatalf("replacement state lost: exists=%t err=%v", exists, err)
	}
}

func TestNamespaceDeleteRejectsStateReplacementAtApprovedPathWhileAwaitingNamespaceLock(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web"})
	statePath := config.ServerStatePath("demoapp", "web")
	approved, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := state.LockNamespaceOperation(context.Background(), config.RegistryPath(), "demoapp")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{stdout: io.Discard, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	done := make(chan error, 1)
	go func() { done <- a.runNamespaceDelete(context.Background(), "demoapp") }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("namespace delete bypassed namespace lock: %v", err)
	case <-time.After(serverOperationLockProbe):
	}

	replacement := approved
	replacement.Compute.ID = "replacement-id"
	replacement.Compute.Name = "replacement"
	if err := state.Save(statePath, replacement); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "namespace destructive authority changed") {
			t.Fatalf("replacement authority error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("namespace delete did not finish after lock release")
	}
	if provider.deleted {
		t.Fatal("replacement reached provider delete")
	}
	current, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("replacement state lost: %v", err)
	}
	if current.Compute.ID != replacement.Compute.ID {
		t.Fatalf("replacement state changed: %+v", current.Compute)
	}
}

func TestNamespaceDeleteRejectsNewStateAtApprovedMissingPath(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"stale"})
	statePath := config.ServerStatePath("demoapp", "stale")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	unlock, err := state.LockNamespaceOperation(context.Background(), config.RegistryPath(), "demoapp")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{stdout: io.Discard, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	done := make(chan error, 1)
	go func() { done <- a.runNamespaceDelete(context.Background(), "demoapp") }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("namespace delete bypassed namespace lock: %v", err)
	case <-time.After(serverOperationLockProbe):
	}

	appeared := state.State{
		Namespace: "demoapp",
		Server:    "stale",
		Compute:   state.ComputeState{Provider: "hetzner", ID: "new-id", Name: "new-server"},
	}
	if err := state.Save(statePath, appeared); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "namespace destructive authority changed") {
			t.Fatalf("new state authority error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("namespace delete did not finish after lock release")
	}
	if provider.deleted {
		t.Fatal("new state reached provider delete")
	}
	if exists, err := state.Exists(statePath); err != nil || !exists {
		t.Fatalf("new state lost: exists=%t err=%v", exists, err)
	}
}

func TestNamespaceDeleteRejectsUnregisteredPartialImport(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web"})
	partialConfig := config.ExampleServer("demoapp", "partial")
	if err := config.Save(config.ServerConfigPath("demoapp", "partial"), partialConfig); err != nil {
		t.Fatal(err)
	}
	partialStatePath := config.ServerStatePath("demoapp", "partial")
	if err := state.Save(partialStatePath, state.State{
		Namespace: "demoapp",
		Server:    "partial",
		Compute:   state.ComputeState{Provider: "hetzner", ID: "partial-id", Name: "demoapp-partial"},
	}); err != nil {
		t.Fatal(err)
	}
	markerPath := partialStatePath + ".import.json"
	if err := os.WriteFile(markerPath, []byte(`{"schema_version":1,"namespace":"demoapp","server":"partial","provider":"hetzner","provider_id":"partial-id"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a := &app{stdout: io.Discard, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	err := a.runNamespaceDelete(context.Background(), "demoapp")
	if err == nil || !strings.Contains(err.Error(), "unregistered local authority") {
		t.Fatalf("unregistered import error = %v", err)
	}
	if provider.deleted {
		t.Fatal("registered provider resource deleted despite partial import authority")
	}
	for _, path := range []string{config.ServerConfigPath("demoapp", "partial"), partialStatePath, markerPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("partial import artifact %s lost: %v", path, err)
		}
	}
}

func TestNamespaceDeleteUnknownNamespaceErrors(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"namespace", "delete", "missing"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "namespace \"missing\" not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestNamespaceDeleteNonInteractiveRequiresYes(t *testing.T) {
	createNamespaceDeleteFixture(t, []string{"web"})
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--non-interactive", "namespace", "delete", "demoapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes required") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestNamespaceDeleteConfirmationCancelled(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web"})
	var out bytes.Buffer
	cmd := New()
	cmd.SetIn(strings.NewReader("no\n"))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"namespace", "delete", "demoapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cancelled") || provider.deleted {
		t.Fatalf("expected cancelled, got %v deleted=%t", err, provider.deleted)
	}
	if !strings.Contains(out.String(), `"status": "planned"`) {
		t.Fatalf("plan preview not printed before confirmation:\n%s", out.String())
	}
}

func TestNamespaceDeleteRecursiveDeletesServersAndNamespace(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web", "api"})
	var out bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.namespaceDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if provider.deletedCount != 2 {
		t.Fatalf("expected 2 provider deletes, got %d", provider.deletedCount)
	}

	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if namespaceExists(reg, "demoapp") {
		t.Fatal("namespace still in registry")
	}
	for _, srv := range []string{"web", "api"} {
		if _, ok := reg.Find("demoapp", srv); ok {
			t.Fatalf("server %s still in registry", srv)
		}
		exists, err := state.Exists(config.Expand(config.ServerStatePath("demoapp", srv)))
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("state for %s still exists", srv)
		}
	}
	if _, err := os.Stat(config.NamespaceConfigDir("demoapp")); !os.IsNotExist(err) {
		t.Fatalf("namespace config dir still exists: %v", err)
	}
	if _, err := os.Stat(config.NamespaceStateDir("demoapp")); !os.IsNotExist(err) {
		t.Fatalf("namespace state dir still exists: %v", err)
	}

	var row namespaceDeleteRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if row.Status != "complete" || row.Namespace != "demoapp" || row.ServerCount != 2 {
		t.Fatalf("bad complete row: %+v", row)
	}
}

func TestNamespaceDeleteStopsOnServerProviderError(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web", "api"})
	provider.deleteDiagnostics = compute.Diagnostics{{Status: compute.Fail, Message: "provider delete failed"}}
	var out bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.namespaceDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"demoapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "provider delete failed") {
		t.Fatalf("expected provider error, got %v", err)
	}
	if provider.deletedCount != 1 {
		t.Fatalf("expected 1 provider delete attempt, got %d", provider.deletedCount)
	}

	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if !namespaceExists(reg, "demoapp") {
		t.Fatal("namespace removed despite server delete failure")
	}
	if _, err := os.Stat(config.NamespaceConfigDir("demoapp")); os.IsNotExist(err) {
		t.Fatal("namespace config dir removed despite failure")
	}
	if _, err := os.Stat(config.NamespaceStateDir("demoapp")); os.IsNotExist(err) {
		t.Fatal("namespace state dir removed despite failure")
	}
}

func TestNamespaceDeleteRemovesLeftoverConfigCredentials(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web"})
	leftover := filepath.Join(config.NamespaceConfigDir("demoapp"), "leftover.yaml")
	if err := os.WriteFile(leftover, []byte("old config"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.namespaceDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !provider.deleted {
		t.Fatal("provider delete not called")
	}
	if _, err := os.Stat(config.NamespaceConfigDir("demoapp")); !os.IsNotExist(err) {
		t.Fatalf("namespace config dir still exists: %v", err)
	}
}

func TestNamespaceDeleteInvokesExternalCleanupPerServer(t *testing.T) {
	provider := createNamespaceDeleteFixture(t, []string{"web"})
	stPath := config.ServerStatePath("demoapp", "web")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Cloudflare = state.CloudflareState{TunnelID: "tun-web", Name: "demoapp-web", Provenance: state.CloudflareTunnelCreated}
	st.Tailscale.NodeID = "node-web"
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	cleanupCalls := 0
	var out bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: io.Discard, yes: true, providers: providerRegistryForPower(t, provider), services: serviceHooks{
		cleanupClients: successfulDeletePreflightClients,
		deleteTrackedExternalResources: func(_ context.Context, cleanup serverDeleteCleanup) (state.State, error) {
			cleanupCalls++
			return cleanup.State, nil
		},
	}}
	cmd := a.namespaceDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("expected 1 external cleanup call, got %d", cleanupCalls)
	}
}
