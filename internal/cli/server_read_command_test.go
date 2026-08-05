package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/state"
	"github.com/spf13/cobra"
)

type readFakeProvider struct {
	cliFakeProvider
	statusDiagnostics compute.Diagnostics
}

func (p readFakeProvider) Status(context.Context, compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	if len(p.statusDiagnostics) > 0 {
		return compute.ServerStatus{}, p.statusDiagnostics
	}
	return compute.ServerStatus{Power: "running", PublicIPv4: "203.0.113.11"}, nil
}

func TestServerStatusUsesGenericProviderAndState(t *testing.T) {
	createServerReadFixture(t)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistry(t)}
	cmd := a.serverStatusCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row serverReadRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("status output is not JSON:\n%s", out.String())
	}
	if row.Namespace != "demoapp" || row.Server != "webapp" || row.Provider != "hetzner" || row.Location != "fsn1" || row.Size != "cpx22" || row.Image != "ubuntu-24.04" || row.Power != "on" || row.PublicIPv4 != "203.0.113.11" || row.Tailscale != "demoapp-webapp" || row.SSH != "ready" || row.Ingress != "none" {
		t.Fatalf("bad status row: %+v", row)
	}
}

func TestServerReadCommandsSupportGenericJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  func(*app) *cobra.Command
		args []string
		want string
	}{
		{name: "list", cmd: func(a *app) *cobra.Command { return a.serverListCmd() }, want: "\"provider\": \"hetzner\""},
		{name: "status", cmd: func(a *app) *cobra.Command { return a.serverStatusCmd() }, args: []string{"webapp"}, want: "\"ssh\": \"ready\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			createServerReadFixture(t)
			var out bytes.Buffer
			a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", jsonOut: true, providers: readProviderRegistry(t)}
			cmd := tc.cmd(a)
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), "hetzner_server") {
				t.Fatalf("bad json output:\n%s", out.String())
			}
		})
	}
}

func TestServerSSHDryRunUsesTailscalePath(t *testing.T) {
	createServerReadFixture(t)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", dryRun: true, providers: readProviderRegistry(t)}
	cmd := a.serverSSHCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row sshDryRunRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("ssh output is not JSON:\n%s", out.String())
	}
	if row.Target != "operator@demoapp-webapp" || len(row.Command) != 3 || row.Command[0] != "tailscale" || row.Command[1] != "ssh" || row.Command[2] != row.Target {
		t.Fatalf("ssh did not use tailscale path: %+v", row)
	}
}

func TestServerSSHPromptsAndPersistsMissingAdminUser(t *testing.T) {
	createTestHome(t)
	cfgPath := config.ServerConfigPath("demoapp", "webapp")
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Admin.Username = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	// Save may still leave empty after defaults change; force empty field on disk if needed.
	if got := serverConfigAdminUsername(cfgPath); got != "" {
		// rewrite file with empty username only
		body := []byte("namespace: demoapp\nserver: webapp\nadmin:\n  username: \"\"\n")
		if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := state.State{Project: "demoapp", Server: "webapp", Compute: state.ComputeState{Provider: "hetzner"}, Tailscale: state.TailscaleState{Name: "demoapp-webapp"}}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "webapp", StatePath: stPath, ConfigPath: cfgPath})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	a := &app{stdin: strings.NewReader("ops\n"), stdout: &out, stderr: io.Discard, dryRun: true, jsonOut: true}
	cmd := a.serverSSHCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row sshDryRunRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("ssh output is not JSON:\n%s", out.String())
	}
	if row.Target != "ops@demoapp-webapp" {
		t.Fatalf("ssh target=%+v", row)
	}
	if got := serverConfigAdminUsername(cfgPath); got != "ops" {
		t.Fatalf("persisted username=%q", got)
	}
}

func TestServerSSHDryRunUsesRegistryConfigPathForAdminUser(t *testing.T) {
	createTestHome(t)
	cfgPath := config.Expand("~/.config/serverpro/custom/webapp.yaml")
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Admin.Username = "operator"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	stPath := config.ServerStatePath("demoapp", "webapp")
	st := state.State{Project: "demoapp", Server: "webapp", Compute: state.ComputeState{Provider: "hetzner", Account: "prod"}, Tailscale: state.TailscaleState{Name: "demoapp-webapp"}}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "webapp", StatePath: stPath, ConfigPath: cfgPath})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, dryRun: true}
	cmd := a.serverSSHCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row sshDryRunRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("ssh output is not JSON:\n%s", out.String())
	}
	if row.Target != "operator@demoapp-webapp" {
		t.Fatalf("ssh ignored registry config path: %+v", row)
	}
}

func TestServerReadCommandsRejectUnsupportedRegistry(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  func(*app) *cobra.Command
	}{
		{name: "status", cmd: func(a *app) *cobra.Command { return a.serverStatusCmd() }},
		{name: "ssh", cmd: func(a *app) *cobra.Command { return a.serverSSHCmd() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			createServerReadFixture(t)
			body := []byte(`{"schema_version":2,"namespaces":{}}`)
			if err := os.WriteFile(config.RegistryPath(), body, 0o600); err != nil {
				t.Fatal(err)
			}
			a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", dryRun: true, providers: readProviderRegistry(t)}
			cmd := tc.cmd(a)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"webapp"})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "unsupported registry schema version") {
				t.Fatalf("expected registry error, got %v", err)
			}
		})
	}
}

func TestServerStatusReportsMissingState(t *testing.T) {
	createTestHome(t)
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistry(t)}
	cmd := a.serverStatusCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("expected missing state error, got %v", err)
	}
}

func TestServerStatusReportsProviderErrors(t *testing.T) {
	createServerReadFixture(t)
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistryWith(t, readFakeProvider{statusDiagnostics: compute.Diagnostics{{Status: compute.Fail, Message: "provider unavailable"}}})}
	cmd := a.serverStatusCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected provider error, got %v", err)
	}
}

func TestServerListReadsGenericComputeState(t *testing.T) {
	createServerReadFixture(t)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistry(t)}
	cmd := a.serverListCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var rows []serverReadRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("list output is not JSON:\n%s", out.String())
	}
	if len(rows) != 1 || rows[0].Namespace != "demoapp" || rows[0].Server != "webapp" || rows[0].Provider != "hetzner" || rows[0].Location != "fsn1" || rows[0].Size != "cpx22" || rows[0].Image != "ubuntu-24.04" {
		t.Fatalf("bad list rows: %+v", rows)
	}
}

func TestServerListRejectsCorruptRegisteredState(t *testing.T) {
	createServerReadFixture(t)
	if err := os.WriteFile(config.ServerStatePath("demoapp", "webapp"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistry(t)}
	cmd := a.serverListCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Fatalf("expected corrupt state error, got %v", err)
	}
}

func TestServerListSkipsMissingRegisteredState(t *testing.T) {
	createTestHome(t)
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "webapp", StatePath: config.ServerStatePath("demoapp", "webapp")})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistry(t)}
	cmd := a.serverListCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Fatalf("stale registry output = %q", out.String())
	}
}

func TestServerListEmptyOutputIsArray(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"server", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Fatalf("empty server list = %q, want []", out.String())
	}
}

func TestServerListProviderFilterStaysLocalWithoutCredentials(t *testing.T) {
	createTestHome(t)
	st := state.State{Project: "demoapp", Server: "webapp", Compute: state.ComputeState{Provider: "hetzner", Namespace: "demoapp", Server: "webapp", ID: "42", Name: "demoapp-webapp"}}
	stPath := config.ServerStatePath("demoapp", "webapp")
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "webapp", StatePath: stPath})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistryWith(t, readFakeProvider{statusDiagnostics: compute.Diagnostics{{Status: compute.Fail, Message: "live status should not run"}}})}
	cmd := a.serverListCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "webapp") {
		t.Fatalf("local list missed server: %s", out.String())
	}
}

func createServerReadFixture(t *testing.T) {
	t.Helper()
	createTestHome(t)
	cfg := config.ExampleServer("demoapp", "webapp")
	cfg.Admin.Username = "operator"
	if err := config.Save(config.ServerConfigPath("demoapp", "webapp"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Project: "demoapp", Server: "webapp", ServerProvider: "acct", Tailscale: "ts"}); err != nil {
		t.Fatal(err)
	}
	st := state.State{
		Namespace: "demoapp",
		Project:   "demoapp",
		Server:    "webapp",
		Compute:   state.ComputeState{Provider: "hetzner", Account: "prod", Namespace: "demoapp", Server: "webapp", ID: "42", Name: "demoapp-webapp", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04", PublicIPv4: "203.0.113.10", ProviderState: map[string]string{"access_policy_id": "9"}},
		Tailscale: state.TailscaleState{Name: "demoapp-webapp", IPs: []string{"100.64.0.1"}},
	}
	if err := state.Save(config.ServerStatePath("demoapp", "webapp"), st); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "webapp", StatePath: config.ServerStatePath("demoapp", "webapp")})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
}

func readProviderRegistry(t *testing.T) *compute.Registry {
	t.Helper()
	return readProviderRegistryWith(t, readFakeProvider{})
}

func readProviderRegistryWith(t *testing.T, provider readFakeProvider) *compute.Registry {
	t.Helper()
	registry := compute.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	return registry
}
