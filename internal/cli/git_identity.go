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
func (a *app) maybeSetupGitHubAccess(ctx context.Context, cfg config.Config, st state.State, sudoPassword string, progress *commandProgress) (config.Config, error) {
	if a.nonInteractive {
		return cfg, nil
	}
	full, err := a.confirmDefault("Set up full GitHub development access (public+private: push, PRs, Actions via gh)?", false)
	if err != nil {
		return cfg, err
	}
	if !full {
		if err := a.maybeSetupGitDeployAccess(ctx, cfg, st, sudoPassword, progress); err != nil {
			return cfg, err
		}
		return a.reloadGitConfig(cfg)
	}
	// Git setup failure must not abort an otherwise healthy server bootstrap;
	// persisted intent lets the same-run and later doctor surface the gap.
	if err := a.setupGitDevIdentity(ctx, cfg, st, sudoPassword, progress); err != nil {
		if _, werr := fmt.Fprintf(a.promptWriter(), "GitHub development setup incomplete: %v\n", err); werr != nil {
			return cfg, werr
		}
	}
	return a.reloadGitConfig(cfg)
}

func (a *app) setupGitDevIdentity(ctx context.Context, cfg config.Config, st state.State, sudoPassword string, progress *commandProgress) error {
	if progress != nil {
		if err := progress.emit(progressPhaseGitIdentity); err != nil {
			return err
		}
	}
	cfg, pat, err := a.promptGitDevIntent(cfg)
	if err != nil {
		return err
	}
	// Publishing non-secret desired state first keeps partial remote mutation
	// discoverable by doctor after any later failure.
	if err := a.persistGitConfig(cfg); err != nil {
		return err
	}
	publicKey, err := a.setupGitAccountKey(ctx, cfg, st, sudoPassword)
	if err != nil {
		return err
	}
	if cfg.Git.DeployRepository != "" {
		// Keep the repository until remote cleanup succeeds so interrupted
		// migrations remain repairable on the next run.
		cfg.Git.DeployRepository = ""
		if err := a.persistGitConfig(cfg); err != nil {
			return err
		}
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
	if cfg.Git.Signing {
		if err := a.setupGitSigning(ctx, cfg, st, sudoPassword); err != nil {
			return err
		}
	}
	if err := a.setupRequiredGitHubCLI(ctx, cfg, st, sudoPassword, pat); err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.promptWriter(), "GitHub development access configured")
	return err
}

func (a *app) promptGitDevIntent(cfg config.Config) (config.Config, string, error) {
	name, err := a.prompt("Git author name")
	if err != nil {
		return cfg, "", err
	}
	email, err := a.prompt("Git author email")
	if err != nil {
		return cfg, "", err
	}
	if name == "" || email == "" {
		return cfg, "", errors.New("git author name and email required")
	}
	signing, err := a.confirmDefault("Set up SSH commit signing?", true)
	if err != nil {
		return cfg, "", err
	}
	pat, err := a.promptSecret("GitHub fine-grained PAT (Contents, Pull requests, Actions, Workflows: read/write)")
	if err != nil {
		return cfg, "", err
	}
	if pat == "" {
		return cfg, "", errors.New("GitHub PAT required for full development access")
	}
	cfg.Git.Identity = config.GitIdentity{Name: name, Email: email}
	cfg.Git.Access = config.GitAccessAccountKey
	cfg.Git.Signing = signing
	return cfg, pat, nil
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

func (a *app) setupGitSigning(ctx context.Context, cfg config.Config, st state.State, sudoPassword string) error {
	publicKey, err := a.setupGitSigningKey(ctx, cfg, st, sudoPassword)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.promptWriter(), "\nAdd this public key as a Signing key:\n\n%s\n\nGitHub: https://github.com/settings/keys > New SSH key, Key type: Signing Key.\n\n", publicKey); err != nil {
		return err
	}
	return a.confirm("Signing key added to your GitHub account?")
}

func (a *app) setupRequiredGitHubCLI(ctx context.Context, cfg config.Config, st state.State, sudoPassword, pat string) error {
	if err := a.setupGitHubCLI(ctx, cfg, st, sudoPassword, pat); err != nil {
		// PAT must never surface through wrapped script errors.
		return redact.New(pat).Error(err)
	}
	_, err := fmt.Fprintln(a.promptWriter(), "gh CLI authenticated")
	return err
}

func (a *app) reloadGitConfig(cfg config.Config) (config.Config, error) {
	updated, err := config.Load(config.Expand(a.resolvedConfigPath(cfg)))
	if err != nil {
		return cfg, fmt.Errorf("reload git configuration: %w", err)
	}
	return updated, nil
}

func (a *app) persistGitConfig(cfg config.Config) error {
	gitIntent := cfg.Git
	if err := config.Update(config.Expand(a.resolvedConfigPath(cfg)), func(current *config.Config) error {
		current.Git = gitIntent
		return current.Validate()
	}); err != nil {
		return fmt.Errorf("save git configuration: %w", err)
	}
	return nil
}
