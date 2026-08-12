package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/spf13/cobra"
)

const serverOperationLockProbe = 50 * time.Millisecond

type powerDeleteFakeProvider struct {
	readFakeProvider
	powerActions      []compute.PowerAction
	powerDeadline     time.Time
	powerResult       *compute.ServerStatus
	powerDiagnostics  compute.Diagnostics
	deleted           bool
	deletedCount      int
	deletedServerID   string
	deleteDeadline    time.Time
	deleteDiagnostics compute.Diagnostics
	deleteReached     chan struct{}
}

func (p *powerDeleteFakeProvider) Power(ctx context.Context, request compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	p.powerActions = append(p.powerActions, request.Action)
	p.powerDeadline, _ = ctx.Deadline()
	if p.powerResult != nil {
		return *p.powerResult, p.powerDiagnostics
	}
	return compute.ServerStatus{Power: "running"}, p.powerDiagnostics
}

func (p *powerDeleteFakeProvider) Delete(ctx context.Context, request compute.DeleteServerRequest) compute.Diagnostics {
	if p.deleteReached != nil {
		close(p.deleteReached)
		p.deleteReached = nil
	}
	p.deleted = true
	p.deletedCount++
	p.deletedServerID = request.Record.ID
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

func TestServerPowerStopsBeforeMutationWithoutApproval(t *testing.T) {
	for _, test := range []struct {
		name string
		app  app
		want string
	}{
		{name: "dry run", app: app{dryRun: true}},
		{name: "non-interactive", app: app{nonInteractive: true}, want: "--yes required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			createServerReadFixture(t)
			provider := &powerDeleteFakeProvider{}
			test.app.stdout = io.Discard
			test.app.stderr = io.Discard
			test.app.project = "demoapp"
			test.app.provider = "hetzner"
			test.app.providers = providerRegistryForPower(t, provider)
			err := test.app.runServerPower(context.Background(), "webapp", compute.PowerStart)
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("error=%v", err)
			}
			if len(provider.powerActions) != 0 {
				t.Fatalf("provider actions=%v", provider.powerActions)
			}
		})
	}
}

func TestServerPowerReportsProviderFailureAndEmptyStatus(t *testing.T) {
	t.Run("provider failure", func(t *testing.T) {
		createServerReadFixture(t)
		provider := &powerDeleteFakeProvider{powerDiagnostics: compute.Diagnostics{{Status: compute.Fail, Message: "power failed"}}}
		a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
		if err := a.runServerPower(context.Background(), "webapp", compute.PowerStart); err == nil || !strings.Contains(err.Error(), "power failed") {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("empty provider status", func(t *testing.T) {
		createServerReadFixture(t)
		var out bytes.Buffer
		empty := compute.ServerStatus{}
		provider := &powerDeleteFakeProvider{powerResult: &empty}
		a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
		if err := a.runServerPower(context.Background(), "webapp", compute.PowerStart); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"status": "complete"`) {
			t.Fatalf("output=%s", out.String())
		}
	})
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

func TestServerDeleteWaitsForExistingServerOperation(t *testing.T) {
	createServerReadFixture(t)
	unlock, err := state.LockServerOperation(context.Background(), config.Expand(config.ServerStatePath("demoapp", "webapp")))
	if err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("delete completed while same-server operation lock was held: %v", err)
	case <-time.After(serverOperationLockProbe):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("delete did not resume after server operation lock was released")
	}
	if !provider.deleted {
		t.Fatal("delete did not reach provider after server operation lock was released")
	}
}

func TestServerDeleteWaitsForNamespaceOperation(t *testing.T) {
	createServerReadFixture(t)
	unlock, err := state.LockNamespaceOperationExclusive(context.Background(), config.RegistryPath(), "demoapp")
	if err != nil {
		t.Fatal(err)
	}
	deleteReached := make(chan struct{})
	provider := &powerDeleteFakeProvider{deleteReached: deleteReached}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("delete bypassed namespace operation lock: %v", err)
	case <-time.After(serverOperationLockProbe):
	}
	select {
	case <-deleteReached:
		unlock()
		t.Fatal("delete reached provider while namespace authority was held")
	default:
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("delete did not resume after namespace lock release")
	}
}

func TestServerDeleteRejectsRegistryReplacementBeforeDestructiveCall(t *testing.T) {
	createServerReadFixture(t)
	approvedStatePath := config.ServerStatePath("demoapp", "webapp")
	approved, err := state.Load(approvedStatePath)
	if err != nil {
		t.Fatal(err)
	}
	replacementStatePath := config.Expand("~/.local/state/serverpro/replacement/state.json")
	replacement := approved
	replacement.Compute.ID = "replacement-id"
	if err := state.Save(replacementStatePath, replacement); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: approved.Namespace, Server: approved.Server, StatePath: replacementStatePath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{provider: "hetzner", providers: providerRegistryForPower(t, provider)}

	err = a.deleteServerDestructive(context.Background(), approved.Server, approvedStatePath, approved)
	if err == nil || !strings.Contains(err.Error(), "destructive authority changed") || provider.deleted {
		t.Fatalf("registry replacement err=%v deleted=%t", err, provider.deleted)
	}
	registry, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	entry, exists := registry.Find(approved.Namespace, approved.Server)
	if !exists || config.Expand(entry.StatePath) != replacementStatePath {
		t.Fatalf("replacement registry entry lost: exists=%t entry=%+v", exists, entry)
	}
}

func TestServerDeleteRejectsMissingRegistryBeforeDestructiveCall(t *testing.T) {
	createServerReadFixture(t)
	approvedStatePath := config.ServerStatePath("demoapp", "webapp")
	approved, err := state.Load(approvedStatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Remove(approved.Namespace, approved.Server)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{provider: "hetzner", providers: providerRegistryForPower(t, provider)}

	err = a.deleteServerDestructive(context.Background(), approved.Server, approvedStatePath, approved)
	if err == nil || !strings.Contains(err.Error(), "destructive authority changed") || provider.deleted {
		t.Fatalf("missing registry err=%v deleted=%t", err, provider.deleted)
	}
	exists, err := state.Exists(config.Expand(approvedStatePath))
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("state removed despite missing registry authority")
	}
}

func TestServerDeleteRejectsFinalAuthorityDriftWhileWaitingForLock(t *testing.T) {
	cases := map[string]func(*testing.T, state.State, string, string){
		"registry state path": func(t *testing.T, oldState state.State, _, oldConfigPath string) {
			replacementStatePath := config.Expand("~/.local/state/serverpro/replacement/state.json")
			replacement := oldState
			replacement.Compute.ID = "replacement-id"
			if err := state.Save(replacementStatePath, replacement); err != nil {
				t.Fatal(err)
			}
			if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
				reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "webapp", StatePath: replacementStatePath, ConfigPath: oldConfigPath})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		},
		"config selector": func(t *testing.T, _ state.State, _, oldConfigPath string) {
			cfg, err := config.LoadPartial(oldConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Access.Tailscale.Tailnet = "replacement.ts.net"
			if err := config.Save(oldConfigPath, cfg); err != nil {
				t.Fatal(err)
			}
		},
		"cleanup credential": func(t *testing.T, _ state.State, _, oldConfigPath string) {
			cfg, err := config.LoadPartial(oldConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			creds, err := credentials.LoadPartial(cfg)
			if err != nil {
				t.Fatal(err)
			}
			creds.Tailscale = "replacement-token"
			if err := credentials.Save(cfg, creds); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			createServerReadFixture(t)
			oldStatePath := config.ServerStatePath("demoapp", "webapp")
			oldState, err := state.Load(oldStatePath)
			if err != nil {
				t.Fatal(err)
			}
			oldState.Tailscale.NodeID = "node-old"
			if err := state.Save(oldStatePath, oldState); err != nil {
				t.Fatal(err)
			}
			oldConfigPath := config.ServerConfigPath("demoapp", "webapp")
			if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
				reg.Upsert(state.RegistryEntry{Namespace: "demoapp", Server: "webapp", StatePath: oldStatePath, ConfigPath: oldConfigPath})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			unlock, err := state.LockServerOperation(context.Background(), oldStatePath)
			if err != nil {
				t.Fatal(err)
			}
			provider := &powerDeleteFakeProvider{}
			a := &app{project: "demoapp", provider: "hetzner", providers: providerRegistryForPower(t, provider), services: serviceHooks{
				deleteTrackedExternalResources: func(context.Context, serverDeleteCleanup) (state.State, error) {
					return state.State{}, nil
				},
			}}
			done := make(chan error, 1)
			go func() { done <- a.deleteServerDestructive(context.Background(), "webapp", oldStatePath, oldState) }()
			select {
			case err := <-done:
				unlock()
				t.Fatalf("delete bypassed operation lock: %v", err)
			case <-time.After(serverOperationLockProbe):
			}
			mutate(t, oldState, oldStatePath, oldConfigPath)
			unlock()

			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "destructive authority changed") || provider.deleted {
					t.Fatalf("authority drift err=%v deleted=%t server=%q", err, provider.deleted, provider.deletedServerID)
				}
			case <-time.After(time.Second):
				t.Fatal("delete did not finish after lock release")
			}
		})
	}
}

func TestServerDeleteRejectsChangedDestructiveAuthority(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	approved, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	current := approved
	current.Compute.ID = "replacement-id"
	if err := state.Save(stPath, current); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{provider: "hetzner", providers: providerRegistryForPower(t, provider)}

	err = a.deleteServerDestructive(context.Background(), "webapp", stPath, approved)
	if err == nil || !strings.Contains(err.Error(), "destructive authority changed") || provider.deleted {
		t.Fatalf("changed authority err=%v deleted=%t", err, provider.deleted)
	}
}

func TestSameServerDeleteAuthorityRejectsEveryDestructiveIdentityChange(t *testing.T) {
	base := state.State{
		Namespace: "demoapp",
		Server:    "webapp",
		Compute: state.ComputeState{
			Provider:         "hetzner",
			ID:               "server-1",
			Name:             "demoapp-webapp",
			ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "policy-1"}},
			ProviderState:    map[string]string{"opaque": "authority-1"},
		},
		Tailscale:  state.TailscaleState{NodeID: "node-1", AuthKeyID: "key-1", Name: "demoapp-webapp", Tags: []string{"tag:serverpro-demoapp"}},
		Cloudflare: state.CloudflareState{TunnelID: "tunnel-1", Name: "demoapp-webapp", Provenance: state.CloudflareTunnelCreated},
	}
	cases := map[string]func(*state.State){
		"provider":           func(st *state.State) { st.Compute.Provider = "vultr" },
		"compute id":         func(st *state.State) { st.Compute.ID = "server-2" },
		"compute name":       func(st *state.State) { st.Compute.Name = "replacement" },
		"managed resource":   func(st *state.State) { st.Compute.ManagedResources[0].ID = "policy-2" },
		"provider state":     func(st *state.State) { st.Compute.ProviderState["opaque"] = "authority-2" },
		"tailscale tailnet":  func(st *state.State) { st.Tailscale.Tailnet = "replacement.ts.net" },
		"tailscale node":     func(st *state.State) { st.Tailscale.NodeID = "node-2" },
		"tailscale auth key": func(st *state.State) { st.Tailscale.AuthKeyID = "key-2" },
		"tailscale name":     func(st *state.State) { st.Tailscale.Name = "replacement" },
		"tailscale tags":     func(st *state.State) { st.Tailscale.Tags[0] = "tag:replacement" },
		"cloudflare tunnel":  func(st *state.State) { st.Cloudflare.TunnelID = "tunnel-2" },
		"cloudflare name":    func(st *state.State) { st.Cloudflare.Name = "replacement" },
		"cloudflare owner":   func(st *state.State) { st.Cloudflare.Provenance = state.CloudflareTunnelAdopted },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			current := base
			current.Compute.ManagedResources = append([]compute.ManagedResourceRef(nil), base.Compute.ManagedResources...)
			current.Compute.ProviderState = map[string]string{"opaque": base.Compute.ProviderState["opaque"]}
			current.Tailscale.Tags = append([]string(nil), base.Tailscale.Tags...)
			mutate(&current)
			if sameServerDeleteAuthority(base, current) {
				t.Fatalf("%s change retained delete authority", name)
			}
		})
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

func TestServerDeleteDryRunListsTrackedExternalCleanup(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale = state.TailscaleState{NodeID: "node-1", AuthKeyID: "key-1", Tags: []string{"tag:serverpro-demoapp"}, PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}}
	st.Cloudflare = state.CloudflareState{TunnelID: "tun-1", Name: "demoapp-webapp", Provenance: state.CloudflareTunnelCreated}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", dryRun: true}
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
	} {
		if !got[want] {
			t.Fatalf("dry-run missing %q in %+v\n%s", want, payload.ExternalCleanup, out.String())
		}
	}
}

func TestServerDeleteDryRunExcludesRetainedCloudflareTunnel(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Cloudflare = state.CloudflareState{TunnelID: "tun-1", Name: "demoapp-webapp", Provenance: state.CloudflareTunnelAdopted}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	resources, err := serverDeleteExternalCleanupPreview(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.Type == deleteResourceCloudflareTunnel {
			t.Fatalf("retained tunnel listed for deletion: %+v", resources)
		}
	}
}

func TestServerDeleteDryRunListsManagedPolicyForEveryProvider(t *testing.T) {
	for _, provider := range []string{"hetzner", "vultr", "digitalocean"} {
		t.Run(provider, func(t *testing.T) {
			createServerReadFixture(t)
			stPath := config.ServerStatePath("demoapp", "webapp")
			st, err := state.Load(stPath)
			if err != nil {
				t.Fatal(err)
			}
			st.Compute.Provider = provider
			st.Compute.ManagedResources = []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "policy-1"}}
			if err := state.Save(stPath, st); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", dryRun: true}
			cmd := a.serverDeleteCmd()
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"webapp"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var row serverOperationRow
			if err := json.Unmarshal(out.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			if id, ok := compute.ManagedResourceID(row.ManagedResources, compute.ManagedResourceAccessPolicy); !ok || id != "policy-1" {
				t.Fatalf("managed resources=%+v", row.ManagedResources)
			}
		})
	}
}

func TestServerDeleteDryRunDoesNotListSharedPolicyAsDeleted(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Tailscale = state.TailscaleState{PolicyTagOwners: []string{"tag:serverpro-demoapp"}, PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-demoapp"}}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	siblingPath := config.ServerStatePath("demoapp", "api")
	sibling := state.State{Namespace: "demoapp", Server: "api", Tailscale: state.TailscaleState{NodeID: "node-2", Tags: []string{"tag:serverpro-demoapp"}}}
	if err := state.Save(siblingPath, sibling); err != nil {
		t.Fatal(err)
	}
	registerCleanupSiblings(t, stPath, siblingPath)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", dryRun: true}
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
	var out, prompts bytes.Buffer
	a := &app{stdin: strings.NewReader("no\n"), stdout: &out, stderr: &prompts, project: "demoapp", provider: "hetzner", providers: providerRegistryForPower(t, provider)}
	cmd := a.serverDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cancelled") || provider.deleted {
		t.Fatalf("expected cancelled delete without provider call, err=%v deleted=%t", err, provider.deleted)
	}
	if !strings.Contains(prompts.String(), "tracked external provider resources") {
		t.Fatalf("confirmation did not mention external resources:\n%s", prompts.String())
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
	exists, err := state.Exists(config.Expand(config.ServerStatePath("demoapp", "webapp")))
	if err != nil {
		t.Fatal(err)
	}
	if exists {
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

func TestServerDeleteRetainsRegistryWhenDurableStateRemovalFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	createServerReadFixture(t)
	stPath := config.Expand(config.ServerStatePath("demoapp", "webapp"))
	unlock, err := state.LockServerOperation(context.Background(), stPath)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	stateDir := filepath.Dir(stPath)
	// Keep the pre-existing workflow lock usable so this fixture isolates the
	// intended failure: local state removal after provider deletion.
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(stateDir, 0o700) }()

	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	err = a.runServerDelete(context.Background(), "webapp")
	if err == nil {
		t.Fatal("delete reported success without durable state removal")
	}
	if !provider.deleted {
		t.Fatalf("provider deletion did not precede local durable removal: %v", err)
	}
	reg, loadErr := state.LoadRegistry(config.RegistryPath())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if _, ok := reg.Find("demoapp", "webapp"); !ok {
		t.Fatal("registry removed after durable state deletion failed")
	}
}

func TestServerDeletePreservesStateWhenExternalCleanupFails(t *testing.T) {
	createServerReadFixture(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(config.ServerConfigPath("demoapp", "webapp"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Namespace: "demoapp", Server: "webapp", ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	stPath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Cloudflare = state.CloudflareState{TunnelID: "tun-1", Name: "demoapp-webapp", Provenance: state.CloudflareTunnelCreated}
	st.Tailscale.NodeID = "node-1"
	st.Tailscale.PolicyTagOwners = []string{"tag:serverpro-demoapp"}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider), services: serviceHooks{
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
	exists, err := state.Exists(config.Expand(stPath))
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
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
