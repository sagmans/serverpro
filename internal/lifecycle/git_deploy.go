package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/remote"
	"github.com/assagman/serverpro/internal/state"
)

func GenerateGitDeployKey(ctx context.Context, r remote.Runner, cfg config.Config, st state.State, repoURL string) (string, error) {
	if r == nil || st.Tailscale.Name == "" {
		return "", fmt.Errorf("remote host unavailable for Git deploy key setup")
	}
	repo, err := parseGitHubSSHRepoURL(repoURL)
	if err != nil {
		return "", err
	}
	out, err := remote.WithTimeout(r, 2*time.Minute).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, gitDeployKeyScript(cfg.Admin.Username, repo))
	if err != nil {
		return "", fmt.Errorf("generate Git deploy key: %w", err)
	}
	publicKey := strings.TrimSpace(out)
	if !strings.HasPrefix(publicKey, "ssh-ed25519 ") {
		return "", fmt.Errorf("generate Git deploy key: unexpected public key output")
	}
	return publicKey, nil
}

func VerifyGitDeployAccess(ctx context.Context, r remote.Runner, cfg config.Config, st state.State, repoURL string) error {
	if r == nil || st.Tailscale.Name == "" {
		return fmt.Errorf("remote host unavailable for Git deploy access verification")
	}
	repoURL, err := NormalizeGitHubSSHRepoURL(repoURL)
	if err != nil {
		return err
	}
	if _, err := remote.WithTimeout(r, 2*time.Minute).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, verifyGitDeployAccessScript(cfg.Admin.Username, repoURL)); err != nil {
		return fmt.Errorf("verify Git deploy access: %w", err)
	}
	return nil
}
