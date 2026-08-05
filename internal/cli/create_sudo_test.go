package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/passwordhash"
	"github.com/sagmans/serverpro/internal/state"
)

func TestCreateNonInteractiveRequiresSudoPasswordBeforeProvision(t *testing.T) {
	cfgPath := createTestConfig(t)
	var calls []string
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error {
			calls = append(calls, "preflight")
			return nil
		},
		runProvision: func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error) {
			calls = append(calls, "runProvision")
			return state.State{}, nil
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "DEMO_WEB_SUDOPASS") {
		t.Fatalf("expected sudo env error, got %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"preflight"}) {
		t.Fatalf("sudo password should be required after preflight but before provision, calls=%v", calls)
	}
	if fileExists(config.RegistryPath()) {
		t.Fatal("missing sudo password should not write registry")
	}
}

func TestCreatePromptClarifiesPasswordIsForNewAdminUser(t *testing.T) {
	cfgPath := createTestConfig(t)
	var out bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", yes: true, stdin: strings.NewReader("correct horse battery staple\n"), stdout: &out, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error) {
			return state.State{}, errors.New("stop after prompt")
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "stop after prompt") {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if !strings.Contains(out.String(), "choose sudo password for new remote admin user: ") {
		t.Fatalf("missing new-user sudo prompt:\n%s", out.String())
	}
}

func TestCreateCommandPassesSudoPasswordHashToProvision(t *testing.T) {
	cfgPath := createTestConfig(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(_ context.Context, got config.Config, stPath string, _ compute.Account, creds credentials.Set, sudoPassword, adminPasswordHash string) (state.State, error) {
			if sudoPassword != "correct horse battery staple" {
				t.Fatalf("sudo password = %q", sudoPassword)
			}
			if !passwordhash.ValidSHA512(adminPasswordHash) || strings.Contains(adminPasswordHash, sudoPassword) {
				t.Fatalf("bad admin password hash %q", adminPasswordHash)
			}
			st := state.State{Project: got.Project, Server: got.Server, Compute: state.ComputeState{ID: "123", Name: got.Compute.Name}, Cloudflare: state.CloudflareState{Name: got.Cloudflare.Tunnel.Name}}
			if err := state.Save(stPath, st); err != nil {
				return state.State{}, err
			}
			return st, nil
		},
		doctorReport: passingCreateDoctorReport,
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
