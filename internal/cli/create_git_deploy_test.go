package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/doctor"
	"github.com/assagman/serverpro/internal/state"
)

func TestCreateOptionalGitDeployAccessGenerateAndVerify(t *testing.T) {
	cfgPath := createTestConfig(t)
	var calls []string
	var verifiedRepo string
	verifyCalls := 0
	var out bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", yes: true, stdin: strings.NewReader("correct horse battery staple\n\ngit@github.com:owner/repo.git\ny\n"), stdout: &out, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(_ context.Context, got config.Config, stPath string, _ compute.Account, creds credentials.Set, sudoPassword, adminPasswordHash string) (state.State, error) {
			calls = append(calls, "runProvision")
			st := state.State{Project: got.Project, Server: got.Server, Tailscale: state.TailscaleState{Name: "demo-web"}, Compute: state.ComputeState{ID: "123", Name: got.Compute.Name}, Cloudflare: state.CloudflareState{Name: got.Cloudflare.Tunnel.Name}}
			if err := state.Save(stPath, st); err != nil {
				return state.State{}, err
			}
			return st, nil
		},
		generateGitDeployKey: func(_ context.Context, got config.Config, st state.State, sudoPassword, repoURL string) (string, error) {
			calls = append(calls, "generateGitDeployKey")
			if st.Tailscale.Name != "demo-web" || sudoPassword != "correct horse battery staple" || repoURL != "git@github.com:owner/repo.git" {
				t.Fatalf("bad generate args: st=%+v sudo=%q repo=%q", st, sudoPassword, repoURL)
			}
			return "ssh-ed25519 AAAATEST serverpro deploy key", nil
		},
		verifyGitDeployAccess: func(_ context.Context, got config.Config, st state.State, sudoPassword, repoURL string) error {
			calls = append(calls, "verifyGitDeployAccess")
			verifiedRepo = repoURL
			verifyCalls++
			if verifyCalls == 1 {
				return errors.New("repository access denied")
			}
			return nil
		},
		doctorReport: func(context.Context, config.Config, state.State, credentials.Set, string, string) doctor.Report {
			calls = append(calls, "doctorReport")
			return doctor.Report{Results: []doctor.Result{{Name: "smoke", Scope: "test", Status: doctor.Pass, Evidence: "ok"}}}
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"runProvision", "verifyGitDeployAccess", "generateGitDeployKey", "verifyGitDeployAccess", "doctorReport"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
	if verifiedRepo != "git@github.com:owner/repo.git" {
		t.Fatalf("verified repo = %q", verifiedRepo)
	}
	if !strings.Contains(out.String(), "Set up Git SSH deploy access for a private GitHub repo? [Y/n]") || !strings.Contains(out.String(), "read-only deploy key") || !strings.Contains(out.String(), "ssh-ed25519 AAAATEST") || !strings.Contains(out.String(), "Git deploy access verified") {
		t.Fatalf("missing Git deploy guidance:\n%s", out.String())
	}
}

func TestCreateOptionalGitDeployAccessSkipsWhenDeclined(t *testing.T) {
	cfgPath := createTestConfig(t)
	var calls []string
	a := &app{configPath: cfgPath, provider: "hetzner", yes: true, stdin: strings.NewReader("correct horse battery staple\nn\n"), stdout: io.Discard, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(_ context.Context, got config.Config, stPath string, _ compute.Account, creds credentials.Set, sudoPassword, adminPasswordHash string) (state.State, error) {
			calls = append(calls, "runProvision")
			st := state.State{Project: got.Project, Server: got.Server, Tailscale: state.TailscaleState{Name: "demo-web"}}
			return st, state.Save(stPath, st)
		},
		generateGitDeployKey: func(context.Context, config.Config, state.State, string, string) (string, error) {
			t.Fatal("generateGitDeployKey should not run")
			return "", nil
		},
		doctorReport: func(context.Context, config.Config, state.State, credentials.Set, string, string) doctor.Report {
			calls = append(calls, "doctorReport")
			return doctor.Report{Results: []doctor.Result{{Name: "smoke", Scope: "test", Status: doctor.Pass, Evidence: "ok"}}}
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"runProvision", "doctorReport"}) {
		t.Fatalf("calls = %v", calls)
	}
}
