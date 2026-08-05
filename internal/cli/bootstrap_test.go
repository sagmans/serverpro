package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func TestBootstrapCommandDefaultsAllAndSkipsGitDeployNonInteractive(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	var gotTarget bootstraptools.Target
	var gotSudo string
	var out bytes.Buffer
	a := &app{configPath: cfgPath, statePath: stPath, nonInteractive: true, stdout: &out, services: serviceHooks{
		bootstrapTools: func(_ context.Context, got config.Config, st state.State, sudoPassword string, target bootstraptools.Target) error {
			gotTarget = target
			gotSudo = sudoPassword
			if got.Project != "demo" || got.Server != "web" || st.Tailscale.Name != "demo-web" {
				t.Fatalf("bad bootstrap args cfg=%+v st=%+v", got, st)
			}
			return nil
		},
		generateGitDeployKey: func(context.Context, config.Config, state.State, string, string) (string, error) {
			t.Fatal("non-interactive bootstrap should skip deploy-key prompt")
			return "", nil
		},
	}}
	cmd := a.serverBootstrapCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotTarget != bootstraptools.TargetAll {
		t.Fatalf("target = %q", gotTarget)
	}
	if gotSudo != "correct horse battery staple" {
		t.Fatalf("sudo password = %q", gotSudo)
	}
	var row serverBootstrapRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("completion output is not JSON: %s", out.String())
	}
	if row.Status != "complete" || row.Action != "bootstrap" || row.Namespace != "demo" || row.Server != "web" || row.Target != "all" || row.Host != "demo-web" {
		t.Fatalf("bad completion output: %+v", row)
	}
}

func TestBootstrapDryRunDoesNotRequireSudoOrRunBootstrap(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	var out bytes.Buffer
	a := &app{configPath: cfgPath, statePath: stPath, dryRun: true, nonInteractive: true, stdout: &out, services: serviceHooks{
		bootstrapTools: func(context.Context, config.Config, state.State, string, bootstraptools.Target) error {
			t.Fatal("dry-run should not run bootstrap")
			return nil
		},
	}}
	cmd := a.serverBootstrapCmd()
	cmd.SetArgs([]string{"web", "mise"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row serverBootstrapRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("dry-run output is not JSON: %s", out.String())
	}
	if row.Status != "planned" || row.Action != "bootstrap" || !row.DryRun || row.Namespace != "demo" || row.Server != "web" || row.Target != "mise" || row.User != "deploy" || row.Host != "demo-web" {
		t.Fatalf("bad dry-run output: %+v", row)
	}
}

func TestBootstrapRejectsInvalidTargetBeforeConfigLoad(t *testing.T) {
	a := &app{stdout: io.Discard}
	cmd := a.serverBootstrapCmd()
	cmd.SetArgs([]string{"web", "gh"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported bootstrap target") {
		t.Fatalf("expected target error, got %v", err)
	}
}

func TestBootstrapHelpDocumentsToolsetAndAuthBoundary(t *testing.T) {
	var out bytes.Buffer
	a := &app{stdout: &out}
	cmd := a.serverBootstrapCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{bootstraptools.DefaultToolsetDescription(), "Pi and gh authentication remain operator-owned"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, out.String())
		}
	}
}
