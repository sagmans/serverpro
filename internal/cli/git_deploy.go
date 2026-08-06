package cli

import (
	"context"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/state"
)

func (a *app) maybeSetupGitDeployAccess(ctx context.Context, cfg config.Config, st state.State, sudoPassword string, progress *commandProgress) error {
	if a.nonInteractive {
		return nil
	}
	ok, err := a.confirmDefault("Set up Git SSH deploy access for a private GitHub repo?", true)
	if err != nil {
		return err
	}
	if !ok {
		cfg.Git = config.Git{Access: config.GitAccessNone}
		return a.persistGitConfig(cfg)
	}
	if progress != nil {
		if err := progress.emit(progressPhaseGitDeploy); err != nil {
			return err
		}
	}
	repoURL, err := a.prompt("Git SSH repository URL (example git@github.com:owner/repo.git)")
	if err != nil {
		return err
	}
	repoURL, err = lifecycle.NormalizeGitHubSSHRepoURL(repoURL)
	if err != nil {
		return err
	}
	cfg.Git = config.Git{Access: config.GitAccessDeployKey, DeployRepository: repoURL}
	// Persisting scope before remote key work keeps an interrupted deploy setup
	// diagnosable and gives future access-mode reconciliation exact ownership.
	if err := a.persistGitConfig(cfg); err != nil {
		return err
	}
	if err := a.verifyGitDeployAccess(ctx, cfg, st, sudoPassword, repoURL); err == nil {
		_, err = fmt.Fprintln(a.promptWriter(), "Git deploy access verified")
		return err
	}
	publicKey, err := a.generateGitDeployKey(ctx, cfg, st, sudoPassword, repoURL)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.promptWriter(), "\nAdd this public key to the GitHub repository as a read-only deploy key:\n\n%s\n\nGitHub: repository Settings > Deploy keys > Add deploy key. Leave write access disabled.\n\n", publicKey); err != nil {
		return err
	}
	if err := a.confirm("Deploy key added in GitHub with read-only access?"); err != nil {
		return err
	}
	if err := a.verifyGitDeployAccess(ctx, cfg, st, sudoPassword, repoURL); err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.promptWriter(), "Git deploy access verified")
	return err
}
