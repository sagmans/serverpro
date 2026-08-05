package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/doctor"
	"github.com/assagman/serverpro/internal/passwordhash"
	"github.com/assagman/serverpro/internal/state"
)

type failStatusProvider struct{ readFakeProvider }

func (failStatusProvider) Status(context.Context, compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider status should not run during dry-run"}}
}

func TestServerDoctorFixRunsFullDoctorReport(t *testing.T) {
	createServerDoctorFixture(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	calls := 0
	a := &app{project: "demo", provider: "hetzner", nonInteractive: true, stdout: io.Discard, providers: readProviderRegistry(t), services: serviceHooks{
		doctorReport: func(_ context.Context, cfg config.Config, st state.State, _ credentials.Set, sudoPassword, adminPasswordHash string) doctor.Report {
			calls++
			if cfg.Project != "demo" || st.Server != "web" || sudoPassword != "correct horse battery staple" || !passwordhash.ValidSHA512(adminPasswordHash) {
				t.Fatalf("bad doctor args cfg=%+v st=%+v sudo=%q hash=%q", cfg, st, sudoPassword, adminPasswordHash)
			}
			return doctor.Report{Results: []doctor.Result{{Name: "sudo", Scope: "remote", Status: doctor.Pass, Evidence: "password sudo enforced"}}}
		},
	}}
	cmd := a.serverDoctorCmd()
	cmd.SetArgs([]string{"web", "--fix"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("doctorReport calls = %d", calls)
	}
}

func TestServerDoctorDryRunSkipsProviderAndStateWrites(t *testing.T) {
	createServerDoctorFixture(t)
	before, err := os.ReadFile(config.Expand(config.ServerStatePath("demo", "web")))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	registry := compute.NewRegistry()
	if err := registry.Register(failStatusProvider{}); err != nil {
		t.Fatal(err)
	}
	a := &app{project: "demo", provider: "hetzner", dryRun: true, nonInteractive: true, stdout: &out, providers: registry}
	cmd := a.serverDoctorCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(config.Expand(config.ServerStatePath("demo", "web")))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("server doctor --dry-run rewrote state")
	}
	var row serverDoctorDryRunRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("dry-run output is not JSON: %s", out.String())
	}
	if row.Status != "planned" || row.Action != "doctor" || !row.DryRun || row.Namespace != "demo" || row.Server != "web" {
		t.Fatalf("bad dry-run output: %+v", row)
	}
}

func TestServerBootstrapUsesUniqueRegistryMatchWithoutNamespace(t *testing.T) {
	createServerDoctorFixture(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	calls := 0
	a := &app{nonInteractive: true, stdout: io.Discard, services: serviceHooks{
		bootstrapTools: func(_ context.Context, cfg config.Config, st state.State, sudoPassword string, target bootstraptools.Target) error {
			calls++
			if cfg.Project != "demo" || cfg.Server != "web" || st.Tailscale.Name != "demo-web" || sudoPassword != "correct horse battery staple" || target != bootstraptools.TargetAll {
				t.Fatalf("bad bootstrap args cfg=%+v st=%+v sudo=%q target=%q", cfg, st, sudoPassword, target)
			}
			return nil
		},
	}}
	cmd := a.serverBootstrapCmd()
	cmd.SetArgs([]string{"web", "all"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("bootstrapTools calls = %d", calls)
	}
}

func createServerDoctorFixture(t *testing.T) {
	t.Helper()
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(config.ServerConfigPath("demo", "web"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Project: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	st := state.State{Project: "demo", Server: "web", Compute: state.ComputeState{Provider: "hetzner", Account: "prod", Namespace: "demo", Server: "web", ID: "42", Name: "demo-web"}, Tailscale: state.TailscaleState{Name: "demo-web"}}
	if err := state.Save(config.ServerStatePath("demo", "web"), st); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demo", Server: "web", StatePath: config.ServerStatePath("demo", "web"), ConfigPath: config.ServerConfigPath("demo", "web")})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
}
