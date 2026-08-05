package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

type gitIdentityHooks struct {
	calls       []string
	verifyFails int
	pat         string
}

func (h *gitIdentityHooks) hooks(t *testing.T) serviceHooks {
	t.Helper()
	return serviceHooks{
		setupGitAccountKey: func(context.Context, config.Config, state.State, string) (string, error) {
			h.calls = append(h.calls, "setupGitAccountKey")
			return "ssh-ed25519 AAAATEST account key", nil
		},
		verifyGitHubSSH: func(context.Context, config.Config, state.State, string) error {
			h.calls = append(h.calls, "verifyGitHubSSH")
			if h.verifyFails > 0 {
				h.verifyFails--
				return errors.New("permission denied (publickey)")
			}
			return nil
		},
		configureGitIdentity: func(context.Context, config.Config, state.State, string) error {
			h.calls = append(h.calls, "configureGitIdentity")
			return nil
		},
		setupGitSigningKey: func(context.Context, config.Config, state.State, string) (string, error) {
			h.calls = append(h.calls, "setupGitSigningKey")
			return "ssh-ed25519 AAAASIGN signing key", nil
		},
		setupGitHubCLI: func(_ context.Context, _ config.Config, _ state.State, _ string, pat string) error {
			h.calls = append(h.calls, "setupGitHubCLI")
			h.pat = pat
			return nil
		},
	}
}

func gitIdentityState() state.State {
	return state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}
}

func TestGitHubAccessFullFlowConfiguresEverything(t *testing.T) {
	cfgPath := createTestConfig(t)
	h := &gitIdentityHooks{}
	var out bytes.Buffer
	stdin := strings.NewReader("y\nbuzz\nbuzz@example.com\ny\ny\ny\nghp_test\n")
	a := &app{configPath: cfgPath, stdin: stdin, stdout: &out, services: h.hooks(t)}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "sudo", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"setupGitAccountKey", "verifyGitHubSSH", "configureGitIdentity", "setupGitSigningKey", "setupGitHubCLI"}
	if strings.Join(h.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", h.calls, want)
	}
	if h.pat != "ghp_test" {
		t.Fatalf("pat = %q", h.pat)
	}
	for _, want := range []string{"Authentication key", "Signing key", "GitHub development access configured"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "ghp_test") {
		t.Fatalf("output leaked PAT:\n%s", out.String())
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Git.Access != "account-key" || !saved.Git.Signing || saved.Git.Identity.Name != "buzz" || saved.Git.Identity.Email != "buzz@example.com" {
		t.Fatalf("persisted git section wrong: %+v", saved.Git)
	}
}

func TestGitHubAccessDeclineFallsBackToDeployKey(t *testing.T) {
	var out bytes.Buffer
	deployCalled := false
	a := &app{stdin: strings.NewReader("n\ny\ngit@github.com:owner/repo.git\n"), stdout: &out, services: serviceHooks{
		verifyGitDeployAccess: func(context.Context, config.Config, state.State, string, string) error {
			return errors.New("repository access denied")
		},
		generateGitDeployKey: func(context.Context, config.Config, state.State, string, string) (string, error) {
			deployCalled = true
			return "", errors.New("stop after fallback proof")
		},
	}}
	err := a.maybeSetupGitHubAccess(context.Background(), config.Config{}, gitIdentityState(), "", nil)
	if err == nil {
		t.Fatal("expected fallback deploy flow error")
	}
	if !deployCalled {
		t.Fatal("declining full access should fall back to deploy key flow")
	}
}

func TestGitHubAccessVerifyRetriesUntilSuccess(t *testing.T) {
	cfgPath := createTestConfig(t)
	h := &gitIdentityHooks{verifyFails: 1}
	var out bytes.Buffer
	stdin := strings.NewReader("y\nbuzz\nbuzz@example.com\ny\ny\nn\n\n")
	a := &app{configPath: cfgPath, stdin: stdin, stdout: &out, services: h.hooks(t)}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "sudo", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "GitHub SSH verification failed") {
		t.Fatalf("missing retry notice:\n%s", out.String())
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Git.Signing {
		t.Fatal("signing declined but persisted as enabled")
	}
}

func TestGitHubAccessSkipsPATWhenEmpty(t *testing.T) {
	cfgPath := createTestConfig(t)
	h := &gitIdentityHooks{}
	var out bytes.Buffer
	stdin := strings.NewReader("y\nbuzz\nbuzz@example.com\ny\nn\n\n")
	a := &app{configPath: cfgPath, stdin: stdin, stdout: &out, services: h.hooks(t)}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "sudo", nil); err != nil {
		t.Fatal(err)
	}
	for _, call := range h.calls {
		if call == "setupGitHubCLI" {
			t.Fatalf("empty PAT should skip gh setup: %v", h.calls)
		}
	}
	if !strings.Contains(out.String(), "Skipped gh CLI authentication") {
		t.Fatalf("missing skip notice:\n%s", out.String())
	}
}

func TestGitHubAccessFailureDoesNotAbortBootstrap(t *testing.T) {
	cfgPath := createTestConfig(t)
	var out bytes.Buffer
	hooks := serviceHooks{
		setupGitAccountKey: func(context.Context, config.Config, state.State, string) (string, error) {
			return "", errors.New("remote unreachable")
		},
	}
	a := &app{configPath: cfgPath, stdin: strings.NewReader("y\nbuzz\nbuzz@example.com\n"), stdout: &out, services: hooks}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "sudo", nil); err != nil {
		t.Fatalf("git setup failure should not abort: %v", err)
	}
	if !strings.Contains(out.String(), "GitHub development setup incomplete") {
		t.Fatalf("missing warning:\n%s", out.String())
	}
}

func TestGitHubAccessSkipsWhenNonInteractive(t *testing.T) {
	a := &app{nonInteractive: true, services: (&gitIdentityHooks{}).hooks(t)}
	if err := a.maybeSetupGitHubAccess(context.Background(), config.Config{}, gitIdentityState(), "", nil); err != nil {
		t.Fatal(err)
	}
}
