package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func TestBootstrapGitRunsDeployAccessByDefaultWhenInteractive(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	var calls []string
	var verifiedRepo string
	verifyCalls := 0
	var out bytes.Buffer
	var progress bytes.Buffer
	a := &app{configPath: cfgPath, statePath: stPath, stdin: strings.NewReader("correct horse battery staple\n\n\ngit@github.com:owner/repo.git\ny\n"), stdout: &out, stderr: &progress, services: serviceHooks{
		bootstrapTools: func(_ context.Context, got config.Config, st state.State, sudoPassword string, target bootstraptools.Target) error {
			calls = append(calls, "bootstrap:"+string(target))
			if target != bootstraptools.TargetGit || sudoPassword != "correct horse battery staple" || st.Tailscale.Name != "demo-web" {
				t.Fatalf("bad bootstrap args target=%q sudo=%q st=%+v", target, sudoPassword, st)
			}
			return nil
		},
		generateGitDeployKey: func(_ context.Context, _ config.Config, _ state.State, _ string, repoURL string) (string, error) {
			calls = append(calls, "generateGitDeployKey:"+repoURL)
			return "ssh-ed25519 AAAATEST serverpro deploy key", nil
		},
		verifyGitDeployAccess: func(_ context.Context, _ config.Config, _ state.State, _ string, repoURL string) error {
			calls = append(calls, "verifyGitDeployAccess")
			verifiedRepo = repoURL
			verifyCalls++
			if verifyCalls == 1 {
				return errors.New("repository access denied")
			}
			return nil
		},
	}}
	cmd := a.serverBootstrapCmd()
	cmd.SetArgs([]string{"web", "git"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"bootstrap:git", "verifyGitDeployAccess", "generateGitDeployKey:git@github.com:owner/repo.git", "verifyGitDeployAccess"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
	if verifiedRepo != "git@github.com:owner/repo.git" {
		t.Fatalf("repo = %q", verifiedRepo)
	}
	var row serverBootstrapRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("bootstrap stdout is not one JSON document: %v\n%s", err, out.String())
	}
	combined := out.String() + progress.String()
	for _, want := range []string{"Set up Git SSH deploy access for a private GitHub repo? [Y/n]", "read-only deploy key", `"status": "complete"`, `"target": "git"`, "progress phase=bootstrap", "progress phase=git-deploy", "attempt=1"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("output missing %q:\n%s", want, combined)
		}
	}
	for _, line := range strings.Split(progress.String(), "\n") {
		if strings.HasPrefix(line, "progress ") && (strings.Contains(line, "correct horse battery staple") || strings.Contains(line, "git@github.com:owner/repo.git")) {
			t.Fatalf("progress leaked user data: %q", line)
		}
	}
}

func TestBootstrapGitSkipsDeployKeyWhenRepoAccessAlreadyWorks(t *testing.T) {
	cfgPath, stPath := writeDoctorFixture(t)
	var calls []string
	var out bytes.Buffer
	a := &app{configPath: cfgPath, statePath: stPath, stdin: strings.NewReader("correct horse battery staple\n\n\ngit@github.com:owner/repo.git\n"), stdout: &out, services: serviceHooks{
		bootstrapTools: func(context.Context, config.Config, state.State, string, bootstraptools.Target) error {
			calls = append(calls, "bootstrap")
			return nil
		},
		generateGitDeployKey: func(context.Context, config.Config, state.State, string, string) (string, error) {
			t.Fatal("existing repo access should skip deploy key generation")
			return "", nil
		},
		verifyGitDeployAccess: func(context.Context, config.Config, state.State, string, string) error {
			calls = append(calls, "verify")
			return nil
		},
	}}
	cmd := a.serverBootstrapCmd()
	cmd.SetArgs([]string{"web", "git"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"bootstrap", "verify"}) {
		t.Fatalf("calls = %v", calls)
	}
	if strings.Contains(out.String(), "Add this public key") {
		t.Fatalf("unexpected deploy key guidance:\n%s", out.String())
	}
}

func TestMaybeSetupGitDeployAccessRejectsInvalidRepoBeforeGeneratingKey(t *testing.T) {
	var out bytes.Buffer
	a := &app{stdin: strings.NewReader("y\nhttps://github.com/owner/repo\n"), stdout: &out, services: serviceHooks{
		generateGitDeployKey: func(context.Context, config.Config, state.State, string, string) (string, error) {
			t.Fatal("generateGitDeployKey should not run for invalid repository URL")
			return "", nil
		},
		verifyGitDeployAccess: func(context.Context, config.Config, state.State, string, string) error {
			t.Fatal("verifyGitDeployAccess should not run for invalid repository URL")
			return nil
		},
	}}
	err := a.maybeSetupGitDeployAccess(context.Background(), config.Config{}, state.State{}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "GitHub SSH") {
		t.Fatalf("expected GitHub SSH URL error, got %v", err)
	}
}
