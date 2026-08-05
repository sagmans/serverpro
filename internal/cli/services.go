package cli

import (
	"context"
	"time"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/doctor"
	"github.com/assagman/serverpro/internal/lifecycle"
	"github.com/assagman/serverpro/internal/provider/cloudflare"
	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/remote"
	"github.com/assagman/serverpro/internal/state"
)

type serviceHooks struct {
	preflight                         func(context.Context, config.Config, credentials.Set) error
	runProvision                      func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error)
	doctorReport                      func(context.Context, config.Config, state.State, credentials.Set, string, string) doctor.Report
	bootstrapTools                    func(context.Context, config.Config, state.State, string, bootstraptools.Target) error
	preflightTrackedExternalResources func(context.Context, *serverDeleteCleanup) error
	deleteTrackedExternalResources    func(context.Context, serverDeleteCleanup) (state.State, error)
	generateGitDeployKey              func(context.Context, config.Config, state.State, string, string) (string, error)
	verifyGitDeployAccess             func(context.Context, config.Config, state.State, string, string) error
}

type tailscalePreflightClient interface {
	TailnetID(context.Context) (string, error)
	Policy(context.Context) (tailscale.Policy, error)
}

func preflightTailscaleAccess(ctx context.Context, client tailscalePreflightClient) error {
	if _, err := client.TailnetID(ctx); err != nil {
		return err
	}
	_, err := client.Policy(ctx)
	return err
}

func (a *app) preflight(ctx context.Context, cfg config.Config, creds credentials.Set) error {
	if a.services.preflight != nil {
		return a.services.preflight(ctx, cfg, creds)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if a.provider != "" {
		provider, err := a.resolveProvider(a.provider)
		if err != nil {
			return err
		}
		accountRef := compute.Account{Name: cfg.Project + "/" + cfg.Server, Provider: provider.Name(), Token: creds.ServerProvider, Scope: cfg.Project + "/" + cfg.Server}
		if diagnostics := provider.Doctor(ctx, accountRef); !diagnostics.Passed() {
			return diagnostics.Err()
		}
		catalog, diagnostics := provider.Catalog(ctx, compute.CatalogQuery{Account: accountRef, Location: cfg.Compute.Location})
		if !diagnostics.Passed() {
			return diagnostics.Err()
		}
		if err := validateCreateCatalogSelection(catalog, cfg); err != nil {
			return err
		}
	}
	if err := preflightTailscaleAccess(ctx, tailscale.New(creds.Tailscale, cfg.Access.Tailscale.Tailnet)); err != nil {
		return err
	}
	if cfg.Cloudflare.Tunnel.Enabled {
		if err := cloudflare.New(creds.Cloudflare, cfg.Cloudflare.AccountID).ValidateAccount(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) runProvision(ctx context.Context, cfg config.Config, stPath string, providerAccount compute.Account, creds credentials.Set, sudoPassword, adminPasswordHash string) (state.State, error) {
	if a.services.runProvision != nil {
		return a.services.runProvision(ctx, cfg, stPath, providerAccount, creds, sudoPassword, adminPasswordHash)
	}
	provider, err := a.resolveProvider(string(providerAccount.Provider))
	if err != nil {
		return state.State{}, err
	}
	return lifecycle.Run(ctx, lifecycle.Options{Config: cfg, ComputeAccount: providerAccount, Creds: creds, StatePath: stPath, AdminPasswordHash: adminPasswordHash, Clients: lifecycle.Clients{
		Compute:    provider,
		Tailscale:  tailscale.New(creds.Tailscale, cfg.Access.Tailscale.Tailnet),
		Cloudflare: cloudflare.New(creds.Cloudflare, cfg.Cloudflare.AccountID),
		Remote:     remote.TailscaleSSH{SudoPassword: sudoPassword},
	}})
}

func (a *app) bootstrapTools(ctx context.Context, cfg config.Config, st state.State, sudoPassword string, target bootstraptools.Target) error {
	if a.services.bootstrapTools != nil {
		return a.services.bootstrapTools(ctx, cfg, st, sudoPassword, target)
	}
	return lifecycle.BootstrapTools(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st, target)
}

func (a *app) generateGitDeployKey(ctx context.Context, cfg config.Config, st state.State, sudoPassword, repoURL string) (string, error) {
	if a.services.generateGitDeployKey != nil {
		return a.services.generateGitDeployKey(ctx, cfg, st, sudoPassword, repoURL)
	}
	return lifecycle.GenerateGitDeployKey(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st, repoURL)
}

func (a *app) verifyGitDeployAccess(ctx context.Context, cfg config.Config, st state.State, sudoPassword, repoURL string) error {
	if a.services.verifyGitDeployAccess != nil {
		return a.services.verifyGitDeployAccess(ctx, cfg, st, sudoPassword, repoURL)
	}
	return lifecycle.VerifyGitDeployAccess(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st, repoURL)
}

func (a *app) doctorReport(ctx context.Context, cfg config.Config, st state.State, creds credentials.Set, sudoPassword, adminPasswordHash string) doctor.Report {
	if a.services.doctorReport != nil {
		return a.services.doctorReport(ctx, cfg, st, creds, sudoPassword, adminPasswordHash)
	}
	provider, accountRef, err := a.serverProviderAccount(st)
	if err != nil {
		return doctor.Report{Results: []doctor.Result{{Status: doctor.Fail, Scope: "provider", Name: "compute provider", Evidence: err.Error()}}}
	}
	return doctor.RunWithOptions(ctx, cfg, st, creds, doctor.Clients{
		Compute:    provider,
		Tailscale:  tailscale.New(creds.Tailscale, cfg.Access.Tailscale.Tailnet),
		Cloudflare: cloudflare.New(creds.Cloudflare, cfg.Cloudflare.AccountID),
		Remote:     remote.TailscaleSSH{SudoPassword: sudoPassword},
	}, doctor.Options{Fix: a.doctorFix, SudoPassword: sudoPassword, SudoPasswordHash: adminPasswordHash, ComputeAccount: accountRef}).Redact(a.redactionSecrets(creds)...)
}
