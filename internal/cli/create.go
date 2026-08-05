package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/passwordhash"
	"github.com/sagmans/serverpro/internal/redact"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/spf13/cobra"
)

type createOverrides struct {
	ComputeName          string
	Location             string
	Size                 string
	Image                string
	AdminUser            string
	TailscaleTailnet     string
	TailscaleTags        string
	Ingress              string
	CloudflareAccountID  string
	CloudflareTunnelName string
	EgressMode           string
}

func (a *app) runCreateCommand(cmd *cobra.Command) error {
	if err := a.validateCreateProviderFlag(); err != nil {
		return err
	}
	if a.dryRun {
		cfg, err := a.loadCreatePreviewConfig()
		if err != nil {
			return err
		}
		return lifecycle.BuildPlan(cfg).Write(a.stdout)
	}
	cfg, stPath, unlockOperation, err := a.prepareConfigForCreate(cmd.Context(), a.project, a.server)
	if err != nil {
		return err
	}
	defer unlockOperation()
	var creds credentials.Set
	creds, _, err = a.ensureCredentials(cfg)
	if err != nil {
		return err
	}
	providerAccount, err := a.computeAccountForConfig(cfg, creds)
	if err != nil {
		return err
	}
	progress := a.newCommandProgress()
	if err := progress.emit(progressPhasePreflight); err != nil {
		return err
	}
	if err := a.preflight(cmd.Context(), cfg, creds); err != nil {
		return err
	}
	if !a.yes {
		if a.nonInteractive {
			return errors.New("--yes required for non-interactive create")
		}
		if err := a.confirm("Create live provider server with Tailscale access and optional ingress, then run doctor?"); err != nil {
			return err
		}
	}
	sudoPassword, err := a.resolveSudoPasswordWithLabel(cfg, "choose sudo password for new remote admin user")
	if err != nil {
		return err
	}
	adminPasswordHash, err := passwordhash.GenerateSHA512(sudoPassword)
	if err != nil {
		return redact.New(a.redactionSecrets(creds)...).Error(err)
	}
	a.addRuntimeSecret(adminPasswordHash)
	unlockTailnet, err := state.LockTailnetPolicy(cmd.Context(), config.RegistryPath(), cfg.Access.Tailscale.Tailnet)
	if err != nil {
		return err
	}
	tailnetLocked := true
	defer func() {
		if tailnetLocked {
			unlockTailnet()
		}
	}()
	if err := a.upsertRegistryEntry(cfg, stPath); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer cancel()
	if err := progress.emit(progressPhaseProvision); err != nil {
		return err
	}
	st, err := a.runProvision(ctx, cfg, stPath, providerAccount, creds, sudoPassword, adminPasswordHash)
	unlockTailnet()
	tailnetLocked = false
	if err != nil {
		return redact.New(a.redactionSecrets(creds)...).Error(err)
	}
	if err := a.maybeSetupGitDeployAccess(ctx, cfg, st, sudoPassword, progress); err != nil {
		return redact.New(a.redactionSecrets(creds)...).Error(err)
	}
	if err := progress.emit(progressPhaseDoctor); err != nil {
		return err
	}
	report := a.doctorReport(ctx, cfg, st, creds, sudoPassword, adminPasswordHash)
	if err := report.Write(a.stdout); err != nil {
		return redact.New(a.redactionSecrets(creds)...).Error(err)
	}
	if !report.Passed() {
		return errors.New("doctor failed")
	}
	return nil
}

func (a *app) validateCreateProviderFlag() error {
	if a.provider == "" {
		return fmt.Errorf("--provider required for server create")
	}
	_, err := a.resolveProvider(a.provider)
	return err
}

func (a *app) computeAccountForConfig(cfg config.Config, creds credentials.Set) (compute.Account, error) {
	provider, err := a.resolveProvider(a.provider)
	if err != nil {
		return compute.Account{}, err
	}
	if creds.ServerProvider == "" {
		return compute.Account{}, fmt.Errorf("missing credentials: [server provider API token]")
	}
	return compute.Account{Name: cfg.Namespace + "/" + cfg.Server, Provider: provider.Name(), Token: creds.ServerProvider, Scope: cfg.Namespace + "/" + cfg.Server}, nil
}

func (a *app) loadCreatePreviewConfig() (config.Config, error) {
	if path := a.initialConfigPath(a.project, a.server); path != "" && fileExists(config.Expand(path)) {
		cfg, err := a.loadConfigForPreview()
		if err != nil {
			return cfg, err
		}
		a.applyCreateOverrides(&cfg)
		return cfg, validateCreatePreviewTarget(cfg)
	}
	if a.configPath != "" {
		return config.Config{}, fmt.Errorf("config file not found at %s", config.Expand(a.configPath))
	}
	if a.project == "" {
		return config.Config{}, fmt.Errorf("namespace required for create --dry-run when config is missing")
	}
	cfg := explicitCreatePreviewConfig(a.project, a.server)
	a.applyCreateOverrides(&cfg)
	return cfg, validateCreatePreviewTarget(cfg)
}

func explicitCreatePreviewConfig(namespace, server string) config.Config {
	cfg := config.ExampleServer(namespace, server)
	cfg.Compute.Location = ""
	cfg.Compute.Size = ""
	cfg.Compute.Image = ""
	return cfg
}

func (a *app) applyCreateOverrides(cfg *config.Config) {
	if a.create.ComputeName != "" {
		cfg.Compute.Name = a.create.ComputeName
	}
	if a.create.Location != "" {
		cfg.Compute.Location = a.create.Location
	}
	if a.create.Size != "" {
		cfg.Compute.Size = a.create.Size
	}
	if a.create.Image != "" {
		cfg.Compute.Image = a.create.Image
	}
	if a.create.AdminUser != "" {
		cfg.Admin.Username = a.create.AdminUser
	}
	if a.create.TailscaleTailnet != "" {
		cfg.Access.Tailscale.Tailnet = a.create.TailscaleTailnet
	}
	if a.create.TailscaleTags != "" {
		cfg.Access.Tailscale.Tags = splitCSV(a.create.TailscaleTags)
	}
	if a.create.Ingress != "" {
		applyIngressSelection(cfg, a.create.Ingress)
	}
	if a.create.CloudflareAccountID != "" {
		cfg.Cloudflare.AccountID = a.create.CloudflareAccountID
	}
	if a.create.CloudflareTunnelName != "" {
		cfg.Cloudflare.Tunnel.Name = a.create.CloudflareTunnelName
	}
	if a.create.EgressMode != "" {
		cfg.Network.Egress.Mode = a.create.EgressMode
	}
}

func validateCreatePreviewTarget(cfg config.Config) error {
	check := cfg
	if check.Cloudflare.AccountID == "" {
		check.Cloudflare.AccountID = "preview"
	}
	return check.Validate()
}
