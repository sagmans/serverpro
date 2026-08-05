package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/passwordhash"
	"github.com/sagmans/serverpro/internal/state"
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
			if cfg.Namespace != "demo" || st.Server != "web" || sudoPassword != "correct horse battery staple" || !passwordhash.ValidSHA512(adminPasswordHash) {
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

func TestDoctorReportUsesInjectedClientsWithProductionOrchestration(t *testing.T) {
	called := false
	cfg := config.ExampleServer("demo", "web")
	st := state.State{Namespace: "demo", Server: "web", Compute: state.ComputeState{Provider: "hetzner", ID: "42", PublicIPv4: "203.0.113.10"}}
	creds := credentials.Set{ServerProvider: "provider-token"}
	a := &app{services: serviceHooks{doctorClients: func(_ context.Context, gotCfg config.Config, gotState state.State, gotCreds credentials.Set, sudoPassword string) (doctor.Clients, compute.Account, error) {
		called = true
		if gotCfg.Namespace != cfg.Namespace || gotState.Compute.ID != st.Compute.ID || gotCreds.ServerProvider != creds.ServerProvider || sudoPassword != "sudo-secret" {
			t.Fatalf("bad doctor client args cfg=%+v state=%+v creds=%+v sudo=%q", gotCfg, gotState, gotCreds, sudoPassword)
		}
		return doctor.Clients{
			Compute: readFakeProvider{},
			PublicSSHProbe: func(context.Context, string) error {
				return syscall.ECONNREFUSED
			},
		}, compute.Account{Provider: "hetzner", Token: gotCreds.ServerProvider}, nil
	}}}
	report := a.doctorReport(context.Background(), cfg, st, creds, "sudo-secret", "")
	if !called {
		t.Fatal("doctor client seam was not used")
	}
	for _, result := range report.Results {
		if result.Name == "compute server" && result.Status == doctor.Pass {
			return
		}
	}
	t.Fatalf("production doctor result missing: %+v", report)
}

func TestServerDoctorSudoRetryRefreshesOnlyRemoteReport(t *testing.T) {
	createServerDoctorFixture(t)
	var out bytes.Buffer
	initialCalls := 0
	retryCalls := 0
	providerResult := doctor.Result{Name: "compute server", Scope: "provider", Status: doctor.Pass, Evidence: "id=42"}
	a := &app{project: "demo", provider: "hetzner", stdin: strings.NewReader("correct horse battery staple\n"), stdout: &out, stderr: io.Discard, providers: readProviderRegistry(t), services: serviceHooks{
		doctorReport: func(context.Context, config.Config, state.State, credentials.Set, string, string) doctor.Report {
			initialCalls++
			return doctor.Report{Results: []doctor.Result{
				providerResult,
				{Name: doctor.SudoPasswordCheckName, Scope: "remote", Status: doctor.Fail, Code: doctor.SudoPasswordAuthFailureCode, Evidence: "sudo password required"},
			}}
		},
		retryDoctorSudoReport: func(_ context.Context, _ config.Config, _ state.State, _ credentials.Set, existing doctor.Report, sudoPassword string) doctor.Report {
			retryCalls++
			if sudoPassword != "correct horse battery staple" || len(existing.Results) != 2 || existing.Results[0] != providerResult {
				t.Fatalf("bad retry args report=%+v sudo=%q", existing, sudoPassword)
			}
			return doctor.Report{Results: []doctor.Result{existing.Results[0], {Name: doctor.SudoPasswordCheckName, Scope: "remote", Status: doctor.Pass, Evidence: "ok"}}}
		},
	}}
	cmd := a.serverDoctorCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if initialCalls != 1 || retryCalls != 1 {
		t.Fatalf("doctor calls initial=%d retry=%d", initialCalls, retryCalls)
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("doctor stdout is not one JSON report: %v\n%s", err, out.String())
	}
	if len(report.Results) != 2 || report.Results[0] != providerResult || report.Results[1].Status != doctor.Pass {
		t.Fatalf("doctor retry report = %+v", report)
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

func TestReportNeedsSudoPasswordDetectsRemoteAuthFailure(t *testing.T) {
	// WHY: server doctor uses this helper to decide whether to reprompt. The
	// classifier must trigger on any matching result without treating generic
	// failures as password-auth failures.
	report := doctor.Report{Results: []doctor.Result{
		{Name: doctor.SudoPasswordCheckName, Status: doctor.Fail, Remediation: doctor.SudoPasswordAuthRemediation},
		{Name: "renamed check", Status: doctor.Fail, Code: doctor.SudoPasswordAuthFailureCode, Remediation: "rewritten remediation"},
	}}
	if !reportNeedsSudoPassword(report) {
		t.Fatal("expected sudo-password reprompt")
	}
	if reportNeedsSudoPassword(doctor.Report{Results: []doctor.Result{{Status: doctor.Pass, Code: doctor.SudoPasswordAuthFailureCode}}}) {
		t.Fatal("passing sudo check must not request password")
	}
}

func TestServerBootstrapUsesUniqueRegistryMatchWithoutNamespace(t *testing.T) {
	createServerDoctorFixture(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	calls := 0
	a := &app{nonInteractive: true, stdout: io.Discard, services: serviceHooks{
		bootstrapTools: func(_ context.Context, cfg config.Config, st state.State, sudoPassword string, target bootstraptools.Target) error {
			calls++
			if cfg.Namespace != "demo" || cfg.Server != "web" || st.Tailscale.Name != "demo-web" || sudoPassword != "correct horse battery staple" || target != bootstraptools.TargetAll {
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
	if err := credentials.Save(cfg, credentials.Set{Namespace: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	st := state.State{Namespace: "demo", Server: "web", Compute: state.ComputeState{Provider: "hetzner", Account: "prod", Namespace: "demo", Server: "web", ID: "42", Name: "demo-web"}, Tailscale: state.TailscaleState{Name: "demo-web"}}
	if err := state.Save(config.ServerStatePath("demo", "web"), st); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Namespace: "demo", Server: "web", StatePath: config.ServerStatePath("demo", "web"), ConfigPath: config.ServerConfigPath("demo", "web")})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
}
