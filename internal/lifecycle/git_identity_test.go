package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

type gitInputRemote struct {
	gitRemote
	inputs []string
}

func (r *gitInputRemote) RunWithInput(_ context.Context, user, host, script, input string) (string, error) {
	r.inputs = append(r.inputs, input)
	return r.Run(context.Background(), user, host, script)
}

func gitIdentityFixture() (config.Config, state.State) {
	cfg := config.Example("prod")
	cfg.Git.Identity = config.GitIdentity{Name: "buzz", Email: "buzz@example.com"}
	return cfg, state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}
}

func TestSetupGitAccountKeyGeneratesDefaultKeyAndAccountHostEntry(t *testing.T) {
	r := &gitRemote{out: "ssh-ed25519 AAAATEST serverpro account key\n"}
	cfg, st := gitIdentityFixture()
	pub, err := SetupGitAccountKey(context.Background(), r, cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 AAAATEST") {
		t.Fatalf("bad public key: %q", pub)
	}
	script := r.scripts[0]
	for _, want := range []string{
		"key_path=\"${ssh_dir}/id_ed25519\"",
		"ssh-keygen -q -t ed25519 -N ''",
		"chmod 0600 \"${key_path}\"",
		"# serverpro github account access",
		"Host github.com",
		"HostName github.com",
		"IdentityFile ~/.ssh/id_ed25519",
		"IdentitiesOnly yes",
		"github_ed25519_key='ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl'",
		"add_known_host 'github.com'",
		"cat \"${key_path}.pub\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "Port 443") {
		t.Fatalf("account key should use standard port 22:\n%s", script)
	}
	if strings.Contains(script, "cat \"${key_path}\"") {
		t.Fatalf("script prints private key:\n%s", script)
	}
}

func TestSetupGitAccountKeyRequiresHost(t *testing.T) {
	cfg, _ := gitIdentityFixture()
	if _, err := SetupGitAccountKey(context.Background(), &gitRemote{}, cfg, state.State{}); err == nil {
		t.Fatal("missing tailscale host should fail")
	}
	if _, err := SetupGitAccountKey(context.Background(), nil, cfg, state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}); err == nil {
		t.Fatal("nil runner should fail")
	}
}

func TestSetupGitAccountKeyRejectsUnexpectedOutput(t *testing.T) {
	cfg, st := gitIdentityFixture()
	if _, err := SetupGitAccountKey(context.Background(), &gitRemote{out: "private-key-material"}, cfg, st); err == nil {
		t.Fatal("unexpected output should fail")
	}
}

func TestVerifyGitHubSSHUsesBatchModeAndAuthMarker(t *testing.T) {
	r := &gitRemote{}
	cfg, st := gitIdentityFixture()
	if err := VerifyGitHubSSH(context.Background(), r, cfg, st); err != nil {
		t.Fatal(err)
	}
	script := r.scripts[0]
	for _, want := range []string{"BatchMode=yes", "ssh -o", "-T git@github.com", "successfully authenticated"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestVerifyGitHubSSHPropagatesFailure(t *testing.T) {
	cfg, st := gitIdentityFixture()
	if err := VerifyGitHubSSH(context.Background(), &gitRemote{err: errors.New("denied")}, cfg, st); err == nil {
		t.Fatal("runner failure should propagate")
	}
}

func TestConfigureGitIdentitySetsNameAndEmail(t *testing.T) {
	r := &gitRemote{}
	cfg, st := gitIdentityFixture()
	if err := ConfigureGitIdentity(context.Background(), r, cfg, st); err != nil {
		t.Fatal(err)
	}
	script := r.scripts[0]
	for _, want := range []string{"git config --global user.name 'buzz'", "git config --global user.email 'buzz@example.com'"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestConfigureGitIdentityRequiresIdentity(t *testing.T) {
	cfg, st := gitIdentityFixture()
	cfg.Git.Identity = config.GitIdentity{}
	if err := ConfigureGitIdentity(context.Background(), &gitRemote{}, cfg, st); err == nil {
		t.Fatal("empty identity should fail")
	}
}

func TestSetupGitSigningKeyConfiguresSSHSigning(t *testing.T) {
	r := &gitRemote{out: "ssh-ed25519 AAAASIGN serverpro signing key\n"}
	cfg, st := gitIdentityFixture()
	pub, err := SetupGitSigningKey(context.Background(), r, cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 AAAASIGN") {
		t.Fatalf("bad public key: %q", pub)
	}
	script := r.scripts[0]
	for _, want := range []string{
		"key_path=\"${ssh_dir}/id_ed25519_sign\"",
		"git_config gpg.format ssh",
		"git_config user.signingkey \"${key_path}.pub\"",
		"git_config commit.gpgsign true",
		"chmod 0600 \"${key_path}\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestSetupGitHubCLIPassesTokenViaStdinOnly(t *testing.T) {
	r := &gitInputRemote{}
	cfg, st := gitIdentityFixture()
	if err := SetupGitHubCLI(context.Background(), r, cfg, st, "ghp_secret"); err != nil {
		t.Fatal(err)
	}
	if len(r.inputs) != 1 || r.inputs[0] != "ghp_secret\n" {
		t.Fatalf("token should reach script via stdin: %+v", r.inputs)
	}
	script := r.scripts[0]
	if strings.Contains(script, "ghp_secret") {
		t.Fatalf("token must not appear in script body:\n%s", script)
	}
	for _, want := range []string{
		"IFS= read -r GH_PAT",
		"GH_TOKEN=\"${GH_PAT}\"",
		"api user --jq .login",
		"oauth_token: ${GH_PAT}",
		"git_protocol: ssh",
		"chmod 0600 \"${hosts_yml}\"",
		"auth status",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestSetupGitHubCLIRequiresToken(t *testing.T) {
	cfg, st := gitIdentityFixture()
	if err := SetupGitHubCLI(context.Background(), &gitInputRemote{}, cfg, st, "  "); err == nil {
		t.Fatal("empty PAT should fail")
	}
}
