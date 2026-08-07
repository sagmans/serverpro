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
	failAt      string
	verifyFails int
	pat         string
}

func (h *gitIdentityHooks) failure(call string) error {
	if h.failAt == call {
		return errors.New("injected " + call + " failure")
	}
	return nil
}

func (h *gitIdentityHooks) hooks(t *testing.T) serviceHooks {
	t.Helper()
	return serviceHooks{
		setupGitAccountKey: func(context.Context, config.Config, state.State, string) (string, error) {
			const call = "setupGitAccountKey"
			h.calls = append(h.calls, call)
			if err := h.failure(call); err != nil {
				return "", err
			}
			return "ssh-ed25519 AAAATEST account key", nil
		},
		verifyGitHubSSH: func(context.Context, config.Config, state.State, string) error {
			const call = "verifyGitHubSSH"
			h.calls = append(h.calls, call)
			if err := h.failure(call); err != nil {
				return err
			}
			if h.verifyFails > 0 {
				h.verifyFails--
				return errors.New("permission denied (publickey)")
			}
			return nil
		},
		configureGitIdentity: func(context.Context, config.Config, state.State, string) error {
			const call = "configureGitIdentity"
			h.calls = append(h.calls, call)
			return h.failure(call)
		},
		setupGitSigningKey: func(context.Context, config.Config, state.State, string) (string, error) {
			const call = "setupGitSigningKey"
			h.calls = append(h.calls, call)
			if err := h.failure(call); err != nil {
				return "", err
			}
			return "ssh-ed25519 AAAASIGN signing key", nil
		},
		setupGitHubCLI: func(_ context.Context, _ config.Config, _ state.State, _ string, pat string) error {
			const call = "setupGitHubCLI"
			h.calls = append(h.calls, call)
			h.pat = pat
			return h.failure(call)
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
	stdin := strings.NewReader("y\nbuzz\nbuzz@example.com\ny\nghp_test\ny\ny\n")
	a := &app{configPath: cfgPath, stdin: stdin, stdout: &out, services: h.hooks(t)}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "sudo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Git.Access != config.GitAccessAccountKey {
		t.Fatalf("returned git intent = %+v", updated.Git)
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
	if saved.Git.Access != config.GitAccessAccountKey || !saved.Git.Signing || saved.Git.Identity.Name != "buzz" || saved.Git.Identity.Email != "buzz@example.com" {
		t.Fatalf("persisted git section wrong: %+v", saved.Git)
	}
}

func TestGitHubAccessClearsReconciledDeployRepository(t *testing.T) {
	cfgPath := createTestConfig(t)
	if err := config.Update(cfgPath, func(current *config.Config) error {
		current.Git = config.Git{
			Access:           config.GitAccessDeployKey,
			DeployRepository: "git@github.com:owner/repo.git",
		}
		return current.Validate()
	}); err != nil {
		t.Fatal(err)
	}
	h := &gitIdentityHooks{}
	a := &app{
		configPath: cfgPath,
		stdin:      strings.NewReader("buzz\nbuzz@example.com\nn\nghp_test\ny\n"),
		stdout:     &bytes.Buffer{},
		services:   h.hooks(t),
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.setupGitDevIdentity(context.Background(), cfg, gitIdentityState(), "sudo", nil); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Git.Access != config.GitAccessAccountKey || saved.Git.DeployRepository != "" {
		t.Fatalf("reconciled git intent = %+v", saved.Git)
	}
}

func TestGitHubAccessDeclineFallsBackToDeployKey(t *testing.T) {
	cfgPath := createTestConfig(t)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	deployCalled := false
	a := &app{configPath: cfgPath, stdin: strings.NewReader("n\ny\ngit@github.com:owner/repo.git\n"), stdout: &out, services: serviceHooks{
		verifyGitDeployAccess: func(context.Context, config.Config, state.State, string, string) error {
			return errors.New("repository access denied")
		},
		generateGitDeployKey: func(context.Context, config.Config, state.State, string, string) (string, error) {
			deployCalled = true
			return "", errors.New("stop after fallback proof")
		},
	}}
	_, err = a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "", nil)
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
	stdin := strings.NewReader("y\nbuzz\nbuzz@example.com\nn\nghp_test\ny\ny\n")
	a := &app{configPath: cfgPath, stdin: stdin, stdout: &out, services: h.hooks(t)}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "sudo", nil); err != nil {
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

func TestGitHubAccessRequiresPATBeforeRemoteMutation(t *testing.T) {
	cfgPath := createTestConfig(t)
	h := &gitIdentityHooks{}
	var out bytes.Buffer
	stdin := strings.NewReader("buzz\nbuzz@example.com\nn\n\n")
	a := &app{configPath: cfgPath, stdin: stdin, stdout: &out, services: h.hooks(t)}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	err = a.setupGitDevIdentity(context.Background(), cfg, gitIdentityState(), "sudo", nil)
	if err == nil || !strings.Contains(err.Error(), "GitHub PAT required") {
		t.Fatalf("empty PAT error = %v", err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("remote mutation ran before required PAT: %v", h.calls)
	}
}

func TestGitHubAccessPersistsIntentBeforeRemoteFailure(t *testing.T) {
	for _, failAt := range []string{"setupGitAccountKey", "verifyGitHubSSH", "configureGitIdentity", "setupGitSigningKey", "setupGitHubCLI"} {
		t.Run(failAt, func(t *testing.T) {
			cfgPath := createTestConfig(t)
			h := &gitIdentityHooks{failAt: failAt}
			stdin := strings.NewReader("buzz\nbuzz@example.com\ny\nghp_test\ny\ny\n")
			a := &app{configPath: cfgPath, stdin: stdin, stdout: &bytes.Buffer{}, services: h.hooks(t)}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.setupGitDevIdentity(context.Background(), cfg, gitIdentityState(), "sudo", nil); err == nil {
				t.Fatal("expected injected remote failure")
			}
			saved, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Git.Access != config.GitAccessAccountKey || !saved.Git.Signing || saved.Git.Identity.Name != "buzz" || saved.Git.Identity.Email != "buzz@example.com" {
				t.Fatalf("persisted intent after %s = %+v", failAt, saved.Git)
			}
		})
	}
}

func TestGitHubAccessRetryExhaustionKeepsPersistedIntent(t *testing.T) {
	cfgPath := createTestConfig(t)
	h := &gitIdentityHooks{verifyFails: gitHubSSHVerifyAttempts}
	stdin := strings.NewReader("buzz\nbuzz@example.com\nn\nghp_test\ny\ny\ny\n")
	a := &app{configPath: cfgPath, stdin: stdin, stdout: &bytes.Buffer{}, services: h.hooks(t)}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	err = a.setupGitDevIdentity(context.Background(), cfg, gitIdentityState(), "sudo", nil)
	if err == nil || !strings.Contains(err.Error(), "not verified after") {
		t.Fatalf("retry exhaustion error = %v", err)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Git.Access != config.GitAccessAccountKey || saved.Git.Identity.Name != "buzz" {
		t.Fatalf("persisted intent after retry exhaustion = %+v", saved.Git)
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
	a := &app{configPath: cfgPath, stdin: strings.NewReader("y\nbuzz\nbuzz@example.com\nn\nghp_test\n"), stdout: &out, services: hooks}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := a.maybeSetupGitHubAccess(context.Background(), cfg, gitIdentityState(), "sudo", nil)
	if err != nil {
		t.Fatalf("git setup failure should not abort: %v", err)
	}
	if updated.Git.Access != config.GitAccessAccountKey {
		t.Fatalf("incomplete setup lost intent: %+v", updated.Git)
	}
	if !strings.Contains(out.String(), "GitHub development setup incomplete") {
		t.Fatalf("missing warning:\n%s", out.String())
	}
}

func TestGitHubAccessSkipsWhenNonInteractive(t *testing.T) {
	a := &app{nonInteractive: true, services: (&gitIdentityHooks{}).hooks(t)}
	if _, err := a.maybeSetupGitHubAccess(context.Background(), config.Config{}, gitIdentityState(), "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestPersistGitConfigPreservesUnrelatedConcurrentUpdate(t *testing.T) {
	cfgPath := createTestConfig(t)
	stale, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	const replacementLocation = "concurrent-location"
	if err := config.Update(cfgPath, func(current *config.Config) error {
		current.Compute.Location = replacementLocation
		return current.Validate()
	}); err != nil {
		t.Fatal(err)
	}
	stale.Git = config.Git{
		Identity: config.GitIdentity{Name: "buzz", Email: "buzz@example.com"},
		Access:   config.GitAccessAccountKey,
	}
	a := &app{configPath: cfgPath}
	if err := a.persistGitConfig(stale); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compute.Location != replacementLocation {
		t.Fatalf("concurrent location = %q, want %q", got.Compute.Location, replacementLocation)
	}
	if got.Git != stale.Git {
		t.Fatalf("git intent = %+v, want %+v", got.Git, stale.Git)
	}
}
