package cli

import (
	"context"
	"io"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/state"
)

func TestCreateCommandGivesDoctorIndependentTimeout(t *testing.T) {
	cfgPath := createTestConfig(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	var provisionCtx context.Context
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(ctx context.Context, cfg config.Config, _ string, _ compute.Account, _ credentials.Set, _, _ string) (state.State, error) {
			provisionCtx = ctx
			return state.State{Namespace: cfg.Namespace, Server: cfg.Server}, nil
		},
		doctorReport: func(ctx context.Context, _ config.Config, _ state.State, _ credentials.Set, _, _ string) doctor.Report {
			if ctx == provisionCtx {
				t.Fatal("doctor reused provisioning context")
			}
			if provisionCtx.Err() != context.Canceled {
				t.Fatalf("provision context error = %v, want canceled", provisionCtx.Err())
			}
			if ctx.Err() != nil {
				t.Fatalf("doctor context started with error: %v", ctx.Err())
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("doctor context has no deadline")
			}
			return passingCreateDoctorReport(ctx, config.Config{}, state.State{}, credentials.Set{}, "", "")
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
