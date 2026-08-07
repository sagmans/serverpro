package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/state"
)

func TestCreateOptionalGitDeployAccessGenerateAndVerify(t *testing.T) {
	cfgPath := createTestConfig(t)
	var calls []string
	var verifiedRepo string
	verifyCalls := 0
	var out bytes.Buffer
	var progress bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", yes: true, stdin: strings.NewReader("correct horse battery staple\n\n\ngit@github.com:owner/repo.git\ny\n"), stdout: &out, stderr: &progress, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(_ context.Context, got config.Config, stPath string, _ compute.Account, creds credentials.Set, sudoPassword, adminPasswordHash string) (state.State, error) {
			calls = append(calls, "runProvision")
			st := state.State{Namespace: got.Namespace, Server: got.Server, Tailscale: state.TailscaleState{Name: "demo-web"}, Compute: state.ComputeState{ID: "123", Name: got.Compute.Name}, Cloudflare: state.CloudflareState{Name: got.Cloudflare.Tunnel.Name}}
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
		doctorReport: func(_ context.Context, got config.Config, _ state.State, _ credentials.Set, _, _ string) doctor.Report {
			calls = append(calls, "doctorReport")
			if got.Git.Access != config.GitAccessDeployKey || got.Git.DeployRepository != "git@github.com:owner/repo.git" {
				t.Fatalf("doctor git intent = %+v", got.Git)
			}
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
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Git.Access != config.GitAccessDeployKey || saved.Git.DeployRepository != verifiedRepo {
		t.Fatalf("persisted deploy intent = %+v", saved.Git)
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("create stdout is not one JSON report: %v\n%s", err, out.String())
	}
	combined := out.String() + progress.String()
	for _, want := range []string{"Set up Git SSH deploy access for a private GitHub repo? [Y/n]", "read-only deploy key", "ssh-ed25519 AAAATEST", "Git deploy access verified", "progress phase=preflight", "progress phase=provision", "progress phase=git-deploy", "progress phase=doctor", "attempt=1"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("create output missing %q:\n%s", want, combined)
		}
	}
	for _, line := range strings.Split(progress.String(), "\n") {
		if strings.HasPrefix(line, "progress ") && (strings.Contains(line, "correct horse battery staple") || strings.Contains(line, "git@github.com:owner/repo.git")) {
			t.Fatalf("progress leaked user data: %q", line)
		}
	}
}

func TestCreateGitAccountAccessRunsDoctorWithPersistedIntent(t *testing.T) {
	cfgPath := createTestConfig(t)
	const accountKey = "ssh-ed25519 AAAATEST account key"
	var out bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", yes: true, stdin: strings.NewReader("correct horse battery staple\ny\nbuzz\nbuzz@example.com\nn\nghp_test\ny\n"), stdout: &out, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(_ context.Context, got config.Config, stPath string, _ compute.Account, _ credentials.Set, _, _ string) (state.State, error) {
			st := state.State{Namespace: got.Namespace, Server: got.Server, Tailscale: state.TailscaleState{Name: "demo-web"}}
			return st, state.Save(stPath, st)
		},
		setupGitAccountKey: func(context.Context, config.Config, state.State, string) (string, error) {
			return accountKey, nil
		},
		verifyGitHubSSH:      func(context.Context, config.Config, state.State, string) error { return nil },
		configureGitIdentity: func(context.Context, config.Config, state.State, string) error { return nil },
		setupGitSigningKey: func(context.Context, config.Config, state.State, string) (string, error) {
			t.Fatal("signing was declined")
			return "", nil
		},
		setupGitHubCLI: func(context.Context, config.Config, state.State, string, string) error { return nil },
		doctorReport: func(_ context.Context, got config.Config, _ state.State, _ credentials.Set, _, _ string) doctor.Report {
			if got.Git.Access != config.GitAccessAccountKey || got.Git.Identity.Name != "buzz" {
				t.Fatalf("doctor received stale git intent: %+v", got.Git)
			}
			return doctor.Report{Results: []doctor.Result{{Name: "smoke", Scope: "test", Status: doctor.Pass, Evidence: "ok"}}}
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "ghp_test") {
		t.Fatalf("create output leaked PAT:\n%s", out.String())
	}
}

func TestCreateOptionalGitDeployAccessSkipsWhenDeclined(t *testing.T) {
	cfgPath := createTestConfig(t)
	var calls []string
	a := &app{configPath: cfgPath, provider: "hetzner", yes: true, stdin: strings.NewReader("correct horse battery staple\nn\nn\n"), stdout: io.Discard, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(_ context.Context, got config.Config, stPath string, _ compute.Account, creds credentials.Set, sudoPassword, adminPasswordHash string) (state.State, error) {
			calls = append(calls, "runProvision")
			st := state.State{Namespace: got.Namespace, Server: got.Server, Tailscale: state.TailscaleState{Name: "demo-web"}}
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
