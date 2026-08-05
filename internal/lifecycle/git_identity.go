package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/state"
)

func gitIdentityRunnerOK(cfg config.Config, st state.State) error {
	if st.Tailscale.Name == "" {
		return fmt.Errorf("remote host unavailable for git identity setup")
	}
	if cfg.Admin.Username == "" {
		return fmt.Errorf("admin user unavailable for git identity setup")
	}
	return nil
}

func SetupGitAccountKey(ctx context.Context, r remote.Runner, cfg config.Config, st state.State) (string, error) {
	if r == nil {
		return "", fmt.Errorf("remote runner unavailable for git account key setup")
	}
	if err := gitIdentityRunnerOK(cfg, st); err != nil {
		return "", err
	}
	out, err := remote.WithTimeout(r, 2*time.Minute).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, gitAccountKeyScript(cfg.Admin.Username))
	if err != nil {
		return "", fmt.Errorf("generate GitHub account key: %w", err)
	}
	publicKey := strings.TrimSpace(out)
	if !strings.HasPrefix(publicKey, "ssh-ed25519 ") {
		return "", fmt.Errorf("generate GitHub account key: unexpected public key output")
	}
	return publicKey, nil
}

func VerifyGitHubSSH(ctx context.Context, r remote.Runner, cfg config.Config, st state.State) error {
	if r == nil {
		return fmt.Errorf("remote runner unavailable for GitHub SSH verification")
	}
	if err := gitIdentityRunnerOK(cfg, st); err != nil {
		return err
	}
	if _, err := remote.WithTimeout(r, 2*time.Minute).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, verifyGitHubSSHScript(cfg.Admin.Username)); err != nil {
		return fmt.Errorf("verify GitHub SSH auth: %w", err)
	}
	return nil
}

func ConfigureGitIdentity(ctx context.Context, r remote.Runner, cfg config.Config, st state.State) error {
	if r == nil {
		return fmt.Errorf("remote runner unavailable for git identity configuration")
	}
	if err := gitIdentityRunnerOK(cfg, st); err != nil {
		return err
	}
	if cfg.Git.Identity.Name == "" || cfg.Git.Identity.Email == "" {
		return fmt.Errorf("git identity name and email required")
	}
	if _, err := remote.WithTimeout(r, 2*time.Minute).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, gitIdentityScript(cfg.Admin.Username, cfg.Git.Identity)); err != nil {
		return fmt.Errorf("configure git identity: %w", err)
	}
	return nil
}

func SetupGitSigningKey(ctx context.Context, r remote.Runner, cfg config.Config, st state.State) (string, error) {
	if r == nil {
		return "", fmt.Errorf("remote runner unavailable for git signing key setup")
	}
	if err := gitIdentityRunnerOK(cfg, st); err != nil {
		return "", err
	}
	out, err := remote.WithTimeout(r, 2*time.Minute).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, gitSigningKeyScript(cfg.Admin.Username))
	if err != nil {
		return "", fmt.Errorf("generate git signing key: %w", err)
	}
	publicKey := strings.TrimSpace(out)
	if !strings.HasPrefix(publicKey, "ssh-ed25519 ") {
		return "", fmt.Errorf("generate git signing key: unexpected public key output")
	}
	return publicKey, nil
}

// SetupGitHubCLI stores the PAT through stdin so it never appears in command
// lines, process lists, or script bodies.
func SetupGitHubCLI(ctx context.Context, r remote.InputRunner, cfg config.Config, st state.State, pat string) error {
	if r == nil {
		return fmt.Errorf("remote runner unavailable for GitHub CLI setup")
	}
	if err := gitIdentityRunnerOK(cfg, st); err != nil {
		return err
	}
	if strings.TrimSpace(pat) == "" {
		return fmt.Errorf("GitHub PAT required")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := r.RunWithInput(ctx, cfg.Admin.Username, st.Tailscale.Name, ghTokenScript(cfg.Admin.Username), pat+"\n"); err != nil {
		return fmt.Errorf("configure GitHub CLI token: %w", err)
	}
	return nil
}
