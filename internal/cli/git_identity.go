package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/redact"
	"github.com/sagmans/serverpro/internal/state"
)

const gitHubSSHVerifyAttempts = 3

// maybeSetupGitHubAccess offers the full development identity first and falls
// back to the read-only deploy key flow, so every interactive run ends up with
// exactly one deliberate GitHub access level.
func (a *app) maybeSetupGitHubAccess(ctx context.Context, cfg config.Config, st state.State, sudoPassword string, progress *commandProgress) error {
	if a.nonInteractive {
		return nil
	}
	full, err := a.confirmDefault("Set up full GitHub development access (public+private: push, PRs, Actions via gh)?", false)
	if err != nil {
		return err
	}
	if !full {
		return a.maybeSetupGitDeployAccess(ctx, cfg, st, sudoPassword, progress)
	}
	// Git setup failure must not abort an otherwise healthy server bootstrap;
	// doctor re-surfaces the gap later.
	if err := a.setupGitDevIdentity(ctx, cfg, st, sudoPassword, progress); err != nil {
		if _, werr := fmt.Fprintf(a.promptWriter(), "GitHub development setup incomplete: %v\n", err); werr != nil {
			return werr
		}
	}
	return nil
}

func (a *app) setupGitDevIdentity(ctx context.Context, cfg config.Config, st state.State, sudoPassword string, progress *commandProgress) error {
	if progress != nil {
		if err := progress.emit(progressPhaseGitIdentity); err != nil {
			return err
		}
	}
	name, err := a.prompt("Git author name")
	if err != nil {
		return err
	}
	email, err := a.prompt("Git author email")
	if err != nil {
		return err
	}
	if name == "" || email == "" {
		return errors.New("git author name and email required")
	}
	cfg.Git.Identity = config.GitIdentity{Name: name, Email: email}
	cfg.Git.Access = "account-key"

	publicKey, err := a.setupGitAccountKey(ctx, cfg, st, sudoPassword)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.promptWriter(), "\nAdd this public key as an Authentication key:\n\n%s\n\nGitHub: https://github.com/settings/keys > New SSH key, Key type: Authentication Key.\n\n", publicKey); err != nil {
		return err
	}
	if err := a.confirm("Authentication key added to your GitHub account?"); err != nil {
		return err
	}
	if err := a.verifyGitHubSSHWithRetry(ctx, cfg, st, sudoPassword); err != nil {
		return err
	}
	if err := a.configureGitIdentity(ctx, cfg, st, sudoPassword); err != nil {
		return err
	}
	if err := a.maybeSetupGitSigning(ctx, &cfg, st, sudoPassword); err != nil {
		return err
	}
	if err := a.maybeSetupGitHubCLI(ctx, cfg, st, sudoPassword); err != nil {
		return err
	}
	if err := a.persistGitConfig(cfg); err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.promptWriter(), "GitHub development access configured")
	return err
}

func (a *app) verifyGitHubSSHWithRetry(ctx context.Context, cfg config.Config, st state.State, sudoPassword string) error {
	var lastErr error
	for attempt := 1; attempt <= gitHubSSHVerifyAttempts; attempt++ {
		if lastErr = a.verifyGitHubSSH(ctx, cfg, st, sudoPassword); lastErr == nil {
			return nil
		}
		if attempt == gitHubSSHVerifyAttempts {
			break
		}
		if _, err := fmt.Fprintf(a.promptWriter(), "GitHub SSH verification failed: %v\n", lastErr); err != nil {
			return err
		}
		if err := a.confirm("Key registered now? Retry verification"); err != nil {
			return err
		}
	}
	return fmt.Errorf("GitHub SSH authentication not verified after %d attempts: %w", gitHubSSHVerifyAttempts, lastErr)
}

func (a *app) maybeSetupGitSigning(ctx context.Context, cfg *config.Config, st state.State, sudoPassword string) error {
	signing, err := a.confirmDefault("Set up SSH commit signing?", true)
	if err != nil || !signing {
		return err
	}
	publicKey, err := a.setupGitSigningKey(ctx, *cfg, st, sudoPassword)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.promptWriter(), "\nAdd this public key as a Signing key:\n\n%s\n\nGitHub: https://github.com/settings/keys > New SSH key, Key type: Signing Key.\n\n", publicKey); err != nil {
		return err
	}
	if err := a.confirm("Signing key added to your GitHub account?"); err != nil {
		return err
	}
	cfg.Git.Signing = true
	return nil
}

func (a *app) maybeSetupGitHubCLI(ctx context.Context, cfg config.Config, st state.State, sudoPassword string) error {
	pat, err := a.promptSecret("GitHub fine-grained PAT (Contents, Pull requests, Actions, Workflows: read/write; empty to skip)")
	if err != nil {
		return err
	}
	if pat == "" {
		_, err := fmt.Fprintln(a.promptWriter(), "Skipped gh CLI authentication")
		return err
	}
	if err := a.setupGitHubCLI(ctx, cfg, st, sudoPassword, pat); err != nil {
		// PAT must never surface through wrapped script errors.
		return redact.New(pat).Error(err)
	}
	_, err = fmt.Fprintln(a.promptWriter(), "gh CLI authenticated")
	return err
}

func (a *app) persistGitConfig(cfg config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(config.Expand(a.resolvedConfigPath(cfg)), cfg); err != nil {
		return fmt.Errorf("save git configuration: %w", err)
	}
	return nil
}
