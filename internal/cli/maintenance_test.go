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

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/state"
)

func TestServerUpgradeFlagValidation(t *testing.T) {
	cases := []struct {
		name   string
		dryRun bool
		args   []string
		want   string
	}{
		{name: "check and security", args: []string{"web", "--check", "--security"}, want: "--check cannot be used with --security"},
		{name: "check and reboot", args: []string{"web", "--check", "--reboot"}, want: "--check cannot be used with reboot flags"},
		{name: "check and fix", args: []string{"web", "--check", "--fix"}, want: "--check cannot be used with --fix"},
		{name: "check and dry-run", dryRun: true, args: []string{"web", "--check"}, want: "--check cannot be used with --dry-run"},
		{name: "fix and dry-run", dryRun: true, args: []string{"web", "--fix"}, want: "--fix cannot be used with --dry-run"},
		{name: "force-reboot without reboot", args: []string{"web", "--force-reboot"}, want: "--force-reboot and --no-wait require --reboot"},
		{name: "no-wait without reboot", args: []string{"web", "--no-wait"}, want: "--force-reboot and --no-wait require --reboot"},
		{name: "no-wait with fix", args: []string{"web", "--reboot", "--no-wait", "--fix"}, want: "--no-wait cannot be used with --fix"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &app{dryRun: tc.dryRun, stdout: io.Discard}
			cmd := a.serverUpgradeCmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestServerUpgradeDryRunNeedsNoSudoOrSSH(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	var out bytes.Buffer
	a := &app{configPath: cfgPath, statePath: stPath, dryRun: true, nonInteractive: true, stdout: &out, services: serviceHooks{
		upgradeServer: func(context.Context, config.Config, state.State, string, lifecycle.UpgradeOptions) (lifecycle.UpgradeResult, error) {
			t.Fatal("dry-run must not execute remote upgrade")
			return lifecycle.UpgradeResult{}, nil
		},
	}}
	cmd := a.serverUpgradeCmd()
	cmd.SetArgs([]string{"web", "--security", "--reboot"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row serverUpgradeRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if row.Status != "planned" || row.Action != "upgrade" || !row.DryRun || row.Mode != lifecycle.UpgradeModeSecurity || !row.RebootPlanned || row.Host != "demo-web" {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestServerUpgradeRequiresYesForNonInteractiveReboot(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, stdout: io.Discard}
	cmd := a.serverUpgradeCmd()
	cmd.SetArgs([]string{"web", "--reboot"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes required for non-interactive upgrade --reboot") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestServerUpgradeExecutesAndEmitsSecretSafeProgress(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	const secretPass = "correct horse battery staple"
	t.Setenv("DEMO_WEB_SUDOPASS", secretPass)
	var out, errBuf bytes.Buffer
	var gotOpts lifecycle.UpgradeOptions
	base := time.Date(2026, time.November, 19, 10, 0, 0, 0, time.UTC)
	tick := -1
	a := &app{
		configPath:     cfgPath,
		statePath:      stPath,
		nonInteractive: true,
		yes:            true,
		stdout:         &out,
		stderr:         &errBuf,
		now: func() time.Time {
			tick++
			return base.Add(time.Duration(tick) * time.Second)
		},
		services: serviceHooks{
			upgradeServer: func(_ context.Context, _ config.Config, _ state.State, sudoPassword string, opts lifecycle.UpgradeOptions) (lifecycle.UpgradeResult, error) {
				if sudoPassword != secretPass {
					t.Fatalf("sudo password mismatch")
				}
				gotOpts = opts
				return lifecycle.UpgradeResult{
					Mode:                   opts.Mode,
					UpgradedPackages:       []string{"libbrotli1=1.1.0-2ubuntu0.1"},
					RebootRequired:         true,
					RebootRequiredPackages: []string{"linux-base"},
					Reboot: &lifecycle.RebootResult{
						Status:       lifecycle.RebootStatusComplete,
						Rebooted:     true,
						Gated:        true,
						BeforeBootID: "boot-1",
						AfterBootID:  "boot-2",
						VerifyStatus: lifecycle.RebootVerifyStatusOK,
					},
				}, nil
			},
		},
	}
	cmd := a.serverUpgradeCmd()
	cmd.SetArgs([]string{"web", "--reboot"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotOpts.Mode != lifecycle.UpgradeModeAll || !gotOpts.Reboot {
		t.Fatalf("unexpected opts: %+v", gotOpts)
	}
	var row serverUpgradeRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out.String())
	}
	if row.Status != "complete" || len(row.UpgradedPackages) != 1 || row.Reboot == nil || row.Reboot.AfterBootID != "boot-2" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if !strings.Contains(errBuf.String(), "progress phase=upgrade elapsed=1s attempt=1") {
		t.Fatalf("missing upgrade progress:\n%s", errBuf.String())
	}
	if strings.Contains(errBuf.String(), secretPass) || strings.Contains(out.String(), secretPass) {
		t.Fatalf("secret leaked in output")
	}
}

func TestServerUpgradeWithFixEmbedsSingleJSONDoctorSummary(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	var out, errBuf bytes.Buffer
	var sawFix bool
	a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, stdout: &out, stderr: &errBuf, services: serviceHooks{
		upgradeServer: func(context.Context, config.Config, state.State, string, lifecycle.UpgradeOptions) (lifecycle.UpgradeResult, error) {
			return lifecycle.UpgradeResult{Mode: lifecycle.UpgradeModeAll, UpgradedPackages: []string{"openssl=3.0.13-0ubuntu3.9"}}, nil
		},
		doctorReport: func(context.Context, config.Config, state.State, credentials.Set, string) doctor.Report {
			return doctor.Report{Results: []doctor.Result{{Status: doctor.Pass, Scope: "remote", Name: "managed packages"}}}
		},
	}}
	// Intercept via wrapper hook to verify a.doctorFix is turned on and restored.
	origDoctor := a.services.doctorReport
	a.services.doctorReport = func(ctx context.Context, cfg config.Config, st state.State, creds credentials.Set, sudo string) doctor.Report {
		sawFix = a.doctorFix && a.doctorStatePath == stPath
		return origDoctor(ctx, cfg, st, creds, sudo)
	}
	cmd := a.serverUpgradeCmd()
	cmd.SetArgs([]string{"web", "--fix"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !sawFix || a.doctorFix {
		t.Fatalf("doctorFix toggle/restore broken: sawFix=%v after=%v", sawFix, a.doctorFix)
	}
	var row serverUpgradeRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("stdout is not single JSON document: %v\n%s", err, out.String())
	}
	if row.Status != "complete" || row.Doctor == nil || row.Doctor.Status != doctor.Pass || row.Doctor.ReportPath == "" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if !strings.Contains(errBuf.String(), "progress phase=doctor") {
		t.Fatalf("missing doctor progress:\n%s", errBuf.String())
	}
}

func TestServerUpgradeWithFixReturnsErrorOnDoctorFailure(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	var out bytes.Buffer
	a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, stdout: &out, services: serviceHooks{
		upgradeServer: func(context.Context, config.Config, state.State, string, lifecycle.UpgradeOptions) (lifecycle.UpgradeResult, error) {
			return lifecycle.UpgradeResult{Mode: lifecycle.UpgradeModeAll}, nil
		},
		doctorReport: func(context.Context, config.Config, state.State, credentials.Set, string) doctor.Report {
			return doctor.Report{Results: []doctor.Result{{Status: doctor.Fail, Scope: "remote", Name: "ufw", Evidence: "inactive"}}}
		},
	}}
	cmd := a.serverUpgradeCmd()
	cmd.SetArgs([]string{"web", "--fix"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "doctor failed") {
		t.Fatalf("expected doctor failed error, got %v", err)
	}
	var row serverUpgradeRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("stdout is not valid JSON on doctor fail: %v\n%s", err, out.String())
	}
	if row.Status != "failed" || row.Doctor == nil || row.Doctor.Status != doctor.Fail {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestServerUpgradeHandlesPreflightBlockAndRedactsErrors(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	const secretPass = "correct horse battery staple"
	t.Setenv("DEMO_WEB_SUDOPASS", secretPass)
	var out bytes.Buffer
	a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, yes: true, stdout: &out, services: serviceHooks{
		upgradeServer: func(context.Context, config.Config, state.State, string, lifecycle.UpgradeOptions) (lifecycle.UpgradeResult, error) {
			return lifecycle.UpgradeResult{
				Mode:             lifecycle.UpgradeModeAll,
				UpgradedPackages: []string{"linux-image-generic=6.8"},
				RebootRequired:   true,
				Reboot:           &lifecycle.RebootResult{BeforeBootID: "boot-1", VerifyFindings: []string{"leaked " + secretPass}},
			}, &lifecycle.RebootPreflightError{Host: "demo-web", Gaps: []string{"container app has restart=no " + secretPass}}
		},
	}}
	cmd := a.serverUpgradeCmd()
	cmd.SetArgs([]string{"web", "--reboot"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "survivability gap") {
		t.Fatalf("expected preflight error, got %v", err)
	}
	if strings.Contains(err.Error(), secretPass) || strings.Contains(out.String(), secretPass) {
		t.Fatalf("sudo password leaked in preflight output/error")
	}
	var row serverUpgradeRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if row.Status != "blocked" || len(row.PreflightGaps) != 1 {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestServerRebootDryRunAndConfirmation(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)

	t.Run("dry-run", func(t *testing.T) {
		var out bytes.Buffer
		a := &app{configPath: cfgPath, statePath: stPath, dryRun: true, nonInteractive: true, stdout: &out}
		cmd := a.serverRebootCmd()
		cmd.SetArgs([]string{"web", "--if-required"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var row serverRebootRow
		if err := json.Unmarshal(out.Bytes(), &row); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out.String())
		}
		if row.Status != "planned" || row.Action != "reboot" || !row.DryRun || !row.IfRequired || !row.Gated {
			t.Fatalf("unexpected row: %+v", row)
		}
	})

	t.Run("requires yes non-interactive", func(t *testing.T) {
		a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, stdout: io.Discard}
		cmd := a.serverRebootCmd()
		cmd.SetArgs([]string{"web"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "--yes required for non-interactive reboot") {
			t.Fatalf("expected --yes error, got %v", err)
		}
	})

	t.Run("interactive confirm declined", func(t *testing.T) {
		a := &app{configPath: cfgPath, statePath: stPath, stdin: strings.NewReader("n\n"), stdout: io.Discard, stderr: io.Discard}
		cmd := a.serverRebootCmd()
		cmd.SetArgs([]string{"web"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected declined confirmation error")
		}
	})
}

func TestServerRebootExecutionAndFailureModes(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)

	t.Run("live complete", func(t *testing.T) {
		t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
		var out, errBuf bytes.Buffer
		a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, yes: true, stdout: &out, stderr: &errBuf, services: serviceHooks{
			rebootServer: func(_ context.Context, _ config.Config, _ state.State, _ string, opts lifecycle.RebootOptions) (lifecycle.RebootResult, error) {
				if !opts.IfRequired || opts.Force {
					t.Fatalf("unexpected reboot opts: %+v", opts)
				}
				return lifecycle.RebootResult{Status: lifecycle.RebootStatusSkipped, RebootRequired: false, Rebooted: false, BeforeBootID: "boot-1"}, nil
			},
		}}
		cmd := a.serverRebootCmd()
		cmd.SetArgs([]string{"web", "--if-required"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var row serverRebootRow
		if err := json.Unmarshal(out.Bytes(), &row); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out.String())
		}
		if row.Status != lifecycle.RebootStatusSkipped || row.Rebooted || row.BeforeBootID != "boot-1" {
			t.Fatalf("unexpected row: %+v", row)
		}
		if !strings.Contains(errBuf.String(), "progress phase=reboot") {
			t.Fatalf("missing reboot progress:\n%s", errBuf.String())
		}
	})

	t.Run("preflight blocked and verify failure emit structured JSON", func(t *testing.T) {
		const secretPass = "correct horse battery staple"
		t.Setenv("DEMO_WEB_SUDOPASS", secretPass)
		var blockedOut bytes.Buffer
		a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, yes: true, stdout: &blockedOut, services: serviceHooks{
			rebootServer: func(context.Context, config.Config, state.State, string, lifecycle.RebootOptions) (lifecycle.RebootResult, error) {
				return lifecycle.RebootResult{BeforeBootID: "boot-1"}, &lifecycle.RebootPreflightError{Host: "demo-web", Gaps: []string{"disabled unit foo"}}
			},
		}}
		cmd := a.serverRebootCmd()
		cmd.SetArgs([]string{"web"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected preflight error")
		}
		var blockedRow serverRebootRow
		if err := json.Unmarshal(blockedOut.Bytes(), &blockedRow); err != nil {
			t.Fatalf("invalid blocked JSON: %v", err)
		}
		if blockedRow.Status != "blocked" || len(blockedRow.PreflightGaps) != 1 {
			t.Fatalf("unexpected blocked row: %+v", blockedRow)
		}

		var failOut bytes.Buffer
		b := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, yes: true, stdout: &failOut, services: serviceHooks{
			rebootServer: func(context.Context, config.Config, state.State, string, lifecycle.RebootOptions) (lifecycle.RebootResult, error) {
				return lifecycle.RebootResult{BeforeBootID: "boot-1", AfterBootID: "boot-2", VerifyStatus: lifecycle.RebootVerifyStatusDegraded}, errors.New("verify degraded " + secretPass)
			},
		}}
		cmd2 := b.serverRebootCmd()
		cmd2.SetArgs([]string{"web"})
		err := cmd2.Execute()
		if err == nil || strings.Contains(err.Error(), secretPass) {
			t.Fatalf("expected redacted verify error, got %v", err)
		}
		var failRow serverRebootRow
		if err := json.Unmarshal(failOut.Bytes(), &failRow); err != nil {
			t.Fatalf("invalid failed JSON: %v", err)
		}
		if failRow.Status != "failed" || failRow.VerifyStatus != lifecycle.RebootVerifyStatusDegraded {
			t.Fatalf("unexpected failed row: %+v", failRow)
		}
	})
}

func TestMaintenanceDefaultServiceDispatchFailsClosedWithoutHost(t *testing.T) {
	a := &app{}
	cfg := config.ExampleServer("demo", "web")
	emptyState := state.State{Namespace: "demo", Server: "web"}
	if _, err := a.upgradeServer(context.Background(), cfg, emptyState, "pass", lifecycle.UpgradeOptions{Mode: lifecycle.UpgradeModeCheck}); err == nil {
		t.Fatal("expected error for empty host on upgradeServer")
	}
	if _, err := a.rebootServer(context.Background(), cfg, emptyState, "pass", lifecycle.RebootOptions{}); err == nil {
		t.Fatal("expected error for empty host on rebootServer")
	}
}
