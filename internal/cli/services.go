package cli

import (
	"context"
	"errors"
	"time"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/importsync"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/state"
)

type preflightTailscaleClient interface {
	Policy(context.Context) (mesh.Policy, error)
}

type preflightCloudflareClient interface {
	ValidateAccount(context.Context) error
}

type serviceHooks struct {
	preflight                      func(context.Context, config.Config, credentials.Set) error
	preflightTailscaleClient       func(string, string) preflightTailscaleClient
	preflightCloudflareClient      func(string, string) preflightCloudflareClient
	runProvision                   func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error)
	provisionClients               func(lifecycle.Clients) lifecycle.Clients
	provisionOptions               func(lifecycle.Options) (lifecycle.Options, error)
	doctorReport                   func(context.Context, config.Config, state.State, credentials.Set, string, string) doctor.Report
	doctorClients                  func(context.Context, config.Config, state.State, credentials.Set, string) (doctor.Clients, compute.Account, error)
	retryDoctorSudoReport          func(context.Context, config.Config, state.State, credentials.Set, doctor.Report, string) doctor.Report
	bootstrapTools                 func(context.Context, config.Config, state.State, string, bootstraptools.Target) error
	deleteTrackedExternalResources func(context.Context, serverDeleteCleanup) (state.State, error)
	cleanupClients                 func(serverDeleteCleanup) serverCleanupClients
	generateGitDeployKey           func(context.Context, config.Config, state.State, string, string) (string, error)
	verifyGitDeployAccess          func(context.Context, config.Config, state.State, string, string) error
	setupGitAccountKey             func(context.Context, config.Config, state.State, string) (string, error)
	verifyGitHubSSH                func(context.Context, config.Config, state.State, string) error
	configureGitIdentity           func(context.Context, config.Config, state.State, string) error
	setupGitSigningKey             func(context.Context, config.Config, state.State, string) (string, error)
	setupGitHubCLI                 func(context.Context, config.Config, state.State, string, string) error
	tailnetPolicyClient            func(string, string) tailnetPolicyClient
}

const tokenRelativeTailnet = "-"

type cleanupCloudflareAdapter struct {
	cloudflare.Client
}

func (c cleanupCloudflareAdapter) DeleteTunnel(ctx context.Context, id string) error {
	err := c.Client.DeleteTunnel(ctx, id)
	if cloudflare.TunnelHasActiveConnections(err) {
		// Keep provider response parsing at composition while cleanup retries a neutral condition.
		return errors.Join(errTunnelActiveConnections, err)
	}
	return err
}

func newServerCleanupClients(cleanup serverDeleteCleanup) serverCleanupClients {
	return serverCleanupClients{
		Tailscale: tailscale.New(cleanup.Creds.Tailscale, cleanup.Config.Access.Tailscale.Tailnet),
		Cloudflare: cleanupCloudflareAdapter{
			Client: cloudflare.New(cleanup.Creds.Cloudflare, cleanup.Config.Cloudflare.AccountID),
		},
	}
}

func (a *app) tailnetPolicyClient(token, tailnet string) tailnetPolicyClient {
	if a.services.tailnetPolicyClient != nil {
		return a.services.tailnetPolicyClient(token, tailnet)
	}
	return tailscale.New(token, tailnet)
}

func wireImportEnrichers(opts *importsync.ImportOptions) {
	// Adapter construction stays in this root so import orchestration remains provider-neutral.
	if opts.WithTailscale {
		tailnet := opts.Tailnet
		if tailnet == "" {
			tailnet = tokenRelativeTailnet
		}
		client := tailscale.New(opts.TailscaleToken, tailnet)
		opts.EnrichTailscale = func(ctx context.Context, candidate importsync.Candidate, cfg config.Config) (state.TailscaleState, error) {
			return importsync.MatchTailscaleDevice(ctx, client, candidate, cfg)
		}
	}
	if opts.WithCloudflare {
		client := cloudflare.New(opts.CloudflareToken, opts.CloudflareAcctID)
		opts.EnrichCloudflare = func(ctx context.Context, candidate importsync.Candidate, cfg config.Config) (state.CloudflareState, error) {
			return importsync.MatchCloudflareTunnel(ctx, client, candidate, cfg)
		}
	}
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
		accountRef := compute.Account{Name: cfg.Namespace + "/" + cfg.Server, Provider: provider.Name(), Token: creds.ServerProvider, Scope: cfg.Namespace + "/" + cfg.Server}
		if diagnostics := provider.Doctor(ctx, accountRef); !diagnostics.Passed() {
			return diagnostics.Err()
		}
	}
	var tailscaleClient preflightTailscaleClient = tailscale.New(creds.Tailscale, cfg.Access.Tailscale.Tailnet)
	if a.services.preflightTailscaleClient != nil {
		tailscaleClient = a.services.preflightTailscaleClient(creds.Tailscale, cfg.Access.Tailscale.Tailnet)
	}
	if _, err := tailscaleClient.Policy(ctx); err != nil {
		return err
	}
	if cfg.Cloudflare.Tunnel.Enabled {
		var cloudflareClient preflightCloudflareClient = cloudflare.New(creds.Cloudflare, cfg.Cloudflare.AccountID)
		if a.services.preflightCloudflareClient != nil {
			cloudflareClient = a.services.preflightCloudflareClient(creds.Cloudflare, cfg.Cloudflare.AccountID)
		}
		if err := cloudflareClient.ValidateAccount(ctx); err != nil {
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
	clients := lifecycle.Clients{
		Compute:    provider,
		Tailscale:  tailscale.New(creds.Tailscale, cfg.Access.Tailscale.Tailnet),
		Cloudflare: cloudflare.New(creds.Cloudflare, cfg.Cloudflare.AccountID),
		Remote:     remote.TailscaleSSH{SudoPassword: sudoPassword},
	}
	if a.services.provisionClients != nil {
		clients = a.services.provisionClients(clients)
	}
	options := lifecycle.Options{Config: cfg, ComputeAccount: providerAccount, Creds: creds, StatePath: stPath, AdminPasswordHash: adminPasswordHash, Clients: clients}
	if a.services.provisionOptions != nil {
		options, err = a.services.provisionOptions(options)
		if err != nil {
			return state.State{}, err
		}
	}
	return lifecycle.Run(ctx, options)
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

func (a *app) setupGitAccountKey(ctx context.Context, cfg config.Config, st state.State, sudoPassword string) (string, error) {
	if a.services.setupGitAccountKey != nil {
		return a.services.setupGitAccountKey(ctx, cfg, st, sudoPassword)
	}
	return lifecycle.SetupGitAccountKey(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st)
}

func (a *app) verifyGitHubSSH(ctx context.Context, cfg config.Config, st state.State, sudoPassword string) error {
	if a.services.verifyGitHubSSH != nil {
		return a.services.verifyGitHubSSH(ctx, cfg, st, sudoPassword)
	}
	return lifecycle.VerifyGitHubSSH(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st)
}

func (a *app) configureGitIdentity(ctx context.Context, cfg config.Config, st state.State, sudoPassword string) error {
	if a.services.configureGitIdentity != nil {
		return a.services.configureGitIdentity(ctx, cfg, st, sudoPassword)
	}
	return lifecycle.ConfigureGitIdentity(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st)
}

func (a *app) setupGitSigningKey(ctx context.Context, cfg config.Config, st state.State, sudoPassword string) (string, error) {
	if a.services.setupGitSigningKey != nil {
		return a.services.setupGitSigningKey(ctx, cfg, st, sudoPassword)
	}
	return lifecycle.SetupGitSigningKey(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st)
}

func (a *app) setupGitHubCLI(ctx context.Context, cfg config.Config, st state.State, sudoPassword, pat string) error {
	if a.services.setupGitHubCLI != nil {
		return a.services.setupGitHubCLI(ctx, cfg, st, sudoPassword, pat)
	}
	return lifecycle.SetupGitHubCLI(ctx, remote.TailscaleSSH{SudoPassword: sudoPassword}, cfg, st, pat)
}

func (a *app) retryDoctorSudoReport(ctx context.Context, cfg config.Config, st state.State, creds credentials.Set, existing doctor.Report, sudoPassword string) doctor.Report {
	if a.services.retryDoctorSudoReport != nil {
		return a.services.retryDoctorSudoReport(ctx, cfg, st, creds, existing, sudoPassword)
	}
	return doctor.RetryRemote(ctx, cfg, st, existing, remote.TailscaleSSH{SudoPassword: sudoPassword}, doctor.Options{SudoPassword: sudoPassword}).Redact(a.redactionSecrets(creds)...)
}

func (a *app) doctorReport(ctx context.Context, cfg config.Config, st state.State, creds credentials.Set, sudoPassword, adminPasswordHash string) doctor.Report {
	if a.services.doctorReport != nil {
		return a.services.doctorReport(ctx, cfg, st, creds, sudoPassword, adminPasswordHash)
	}
	var clients doctor.Clients
	var accountRef compute.Account
	var err error
	if a.services.doctorClients != nil {
		clients, accountRef, err = a.services.doctorClients(ctx, cfg, st, creds, sudoPassword)
	} else {
		clients.Compute, accountRef, err = a.serverProviderAccount(st)
		clients.Tailscale = tailscale.New(creds.Tailscale, cfg.Access.Tailscale.Tailnet)
		clients.Cloudflare = cloudflare.New(creds.Cloudflare, cfg.Cloudflare.AccountID)
		clients.Remote = remote.TailscaleSSH{SudoPassword: sudoPassword}
	}
	if err != nil {
		return doctor.Report{Results: []doctor.Result{{Status: doctor.Fail, Scope: "provider", Name: "compute provider", Evidence: err.Error()}}}
	}
	return doctor.RunWithOptions(ctx, cfg, st, creds, clients, doctor.Options{Fix: a.doctorFix, SudoPassword: sudoPassword, SudoPasswordHash: adminPasswordHash, ComputeAccount: accountRef}).Redact(a.redactionSecrets(creds)...)
}
