package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func TestGenerateGitDeployKeyCreatesPerRepoTargetUserKeyWithoutPrintingPrivateMaterial(t *testing.T) {
	r := &gitRemote{out: "ssh-ed25519 AAAATEST serverpro deploy key\n"}
	cfg := config.Example("prod")
	st := state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}
	pub, err := GenerateGitDeployKey(context.Background(), r, cfg, st, "git@github.com:example/example-app.git")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 AAAATEST") {
		t.Fatalf("bad public key: %q", pub)
	}
	script := r.scripts[0]
	for _, want := range []string{
		"serverpro_deploy_key_example_example-app",
		"serverpro deploy key example/example-app",
		"ssh-keygen -q -t ed25519 -N ''",
		"cat \"${key_path}.pub\"",
		"# serverpro git deploy access example/example-app",
		"Host serverpro-github-example-example-app",
		"HostName ssh.github.com",
		"Port 443",
		"IdentityFile ~/.ssh/serverpro_deploy_key_example_example-app",
		"env HOME=\"${TARGET_HOME}\" git config --global --replace-all",
		"url.git@serverpro-github-example-example-app:example/example-app.git.insteadOf",
		"git@github.com:example/example-app.git",
		"github_ed25519_key='ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl'",
		"SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU",
		"add_known_host '[ssh.github.com]:443'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "cat \"${key_path}\"") {
		t.Fatalf("script prints private key:\n%s", script)
	}
	if strings.Contains(script, "ssh-keyscan") {
		t.Fatalf("script trusts live host key scan:\n%s", script)
	}
}

func TestVerifyGitDeployAccessRequiresGitHubSSHURLAndUsesConfiguredIdentity(t *testing.T) {
	r := &gitRemote{}
	cfg := config.Example("prod")
	st := state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}
	if err := VerifyGitDeployAccess(context.Background(), r, cfg, st, "git@github.com:owner/repo.git"); err != nil {
		t.Fatal(err)
	}
	script := r.scripts[0]
	for _, want := range []string{
		"TARGET_HOME=\"$(printf '%s' \"${user_record}\" | cut -d: -f6)\"",
		"env HOME=\"${TARGET_HOME}\" GIT_SSH_COMMAND='ssh -F ~/.ssh/config -o BatchMode=yes'",
		"git ls-remote",
		"git@github.com:owner/repo.git",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("verify script missing %q:\n%s", want, script)
		}
	}
	if err := VerifyGitDeployAccess(context.Background(), r, cfg, st, "https://github.com/owner/repo"); err == nil || !strings.Contains(err.Error(), "GitHub SSH") {
		t.Fatalf("expected GitHub SSH URL error, got %v", err)
	}
}

func TestGenerateGitDeployKeyRejectsUnexpectedOutput(t *testing.T) {
	r := &gitRemote{out: "private-ish output\n"}
	cfg := config.Example("prod")
	st := state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}
	if _, err := GenerateGitDeployKey(context.Background(), r, cfg, st, "git@github.com:owner/repo.git"); err == nil || !strings.Contains(err.Error(), "unexpected public key") {
		t.Fatalf("expected unexpected public key error, got %v", err)
	}
}

type gitRemote struct {
	out     string
	err     error
	user    string
	host    string
	scripts []string
}

func (r *gitRemote) Run(_ context.Context, user, host, script string) (string, error) {
	r.user = user
	r.host = host
	r.scripts = append(r.scripts, script)
	if r.err != nil {
		return r.out, r.err
	}
	if r.out == "" {
		return "ok", nil
	}
	return r.out, nil
}
