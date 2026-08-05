package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/importsync"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/spf13/cobra"
)

type importFlags struct {
	providerID          string
	adminUser           string
	tailscaleTailnet    string
	cloudflareAccountID string
	withTailscale       bool
	withCloudflare      bool
	force               bool
	includeUnmanaged    bool
}

func (a *app) serverDiscoverCmd() *cobra.Command {
	flags := &importFlags{}
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "list provider servers labeled for serverpro",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServerDiscover(cmd.Context(), flags)
		},
	}
	cmd.Flags().StringVar(&flags.providerID, "provider-id", "", "filter by provider resource id")
	cmd.Flags().BoolVar(&flags.includeUnmanaged, "include-unmanaged", false, "include servers without serverpro ownership labels")
	return cmd
}

func (a *app) serverImportCmd() *cobra.Command {
	flags := &importFlags{}
	cmd := &cobra.Command{
		Use:   "import [NAME]",
		Short: "rebuild local config and state from labeled provider servers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return a.runServerImport(cmd.Context(), name, flags)
		},
	}
	cmd.Flags().StringVar(&flags.providerID, "provider-id", "", "import by provider resource id")
	cmd.Flags().StringVar(&flags.adminUser, "admin-user", "", "remote admin username (prompted if omitted)")
	cmd.Flags().StringVar(&flags.tailscaleTailnet, "tailscale-tailnet", "", "Tailscale tailnet for enrichment")
	cmd.Flags().StringVar(&flags.cloudflareAccountID, "cloudflare-account-id", "", "Cloudflare account ID for tunnel enrichment")
	cmd.Flags().BoolVar(&flags.withTailscale, "with-tailscale", false, "rediscover Tailscale device metadata")
	cmd.Flags().BoolVar(&flags.withCloudflare, "with-cloudflare", false, "rediscover Cloudflare tunnel metadata")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite existing local state")
	return cmd
}

func (a *app) runServerDiscover(ctx context.Context, flags *importFlags) error {
	provider, account, err := a.importProviderAccount()
	if err != nil {
		return err
	}
	candidates, err := importsync.Discover(ctx, provider, account, importsync.DiscoverFilter{
		Namespace:        a.project,
		Server:           a.server,
		ProviderID:       flags.providerID,
		IncludeUnmanaged: flags.includeUnmanaged,
	})
	if err != nil {
		return err
	}
	return writeJSON(a.stdout, candidates)
}

func (a *app) runServerImport(ctx context.Context, name string, flags *importFlags) error {
	if name == "" && flags.providerID == "" && !a.all {
		return fmt.Errorf("import requires NAME, --provider-id, or --all")
	}
	if name != "" && a.all {
		return fmt.Errorf("import NAME and --all are mutually exclusive")
	}
	if flags.providerID != "" && a.all {
		return fmt.Errorf("--provider-id and --all are mutually exclusive")
	}
	if flags.withCloudflare && flags.cloudflareAccountID == "" {
		return fmt.Errorf("--cloudflare-account-id required with --with-cloudflare")
	}
	provider, account, err := a.importProviderAccount()
	if err != nil {
		return err
	}
	candidates, err := importsync.Discover(ctx, provider, account, importsync.DiscoverFilter{
		Namespace:  a.project,
		Server:     name,
		ProviderID: flags.providerID,
	})
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no managed servers matched import criteria")
	}
	if name != "" && a.project == "" && len(candidates) > 1 {
		return fmt.Errorf("server %q is ambiguous across namespaces; pass --namespace", name)
	}
	if !a.dryRun {
		if err := a.confirmImport(len(candidates)); err != nil {
			return err
		}
	}
	if err := a.resolveImportAdminUser(flags); err != nil {
		return err
	}
	opts, err := a.buildImportOptions(candidates, account.Token, flags)
	if err != nil {
		return err
	}
	results, err := importsync.ImportAll(ctx, opts)
	if err != nil {
		return err
	}
	if err := writeJSON(a.stdout, results); err != nil {
		return err
	}
	failed := 0
	for _, result := range results {
		if result.Status == "failed" {
			failed++
		}
	}
	if failed > 0 {
		suffix := "s"
		if failed == 1 {
			suffix = ""
		}
		return fmt.Errorf("%d server import%s failed", failed, suffix)
	}
	return nil
}

func (a *app) importProviderAccount() (compute.Provider, compute.Account, error) {
	if a.provider == "" {
		return nil, compute.Account{}, fmt.Errorf("--provider/-p required")
	}
	provider, err := a.resolveProvider(a.provider)
	if err != nil {
		return nil, compute.Account{}, err
	}
	account, err := a.ephemeralComputeAccount(provider)
	if err != nil {
		return nil, compute.Account{}, err
	}
	return provider, account, nil
}

func (a *app) confirmImport(count int) error {
	if a.yes {
		return nil
	}
	return a.confirm(fmt.Sprintf("import %d server(s) into local config/state", count))
}

func (a *app) resolveImportAdminUser(flags *importFlags) error {
	if strings.TrimSpace(flags.adminUser) != "" {
		return nil
	}
	if a.dryRun {
		return nil
	}
	// WHY: imported hosts may not use the create-time default; force an explicit user.
	user, err := a.prompt("admin username")
	if err != nil {
		return err
	}
	if strings.TrimSpace(user) == "" {
		return fmt.Errorf("admin username required")
	}
	flags.adminUser = strings.TrimSpace(user)
	return nil
}

func (a *app) buildImportOptions(candidates []importsync.Candidate, providerToken string, flags *importFlags) (importsync.ImportOptions, error) {
	opts := importsync.ImportOptions{
		Candidates:       candidates,
		ProviderToken:    providerToken,
		AdminUser:        flags.adminUser,
		Tailnet:          flags.tailscaleTailnet,
		WithTailscale:    flags.withTailscale,
		WithCloudflare:   flags.withCloudflare,
		CloudflareAcctID: flags.cloudflareAccountID,
		Force:            flags.force,
		DryRun:           a.dryRun,
	}
	// Mesh is default intent; collect token for writes and optional device enrich.
	if flags.withTailscale || !a.dryRun {
		token, err := a.resolveOptionalServiceToken("Tailscale API token", "SERVERPRO_TAILSCALE_TOKEN", "TAILSCALE_API_TOKEN", false)
		if err != nil {
			return importsync.ImportOptions{}, err
		}
		opts.TailscaleToken = token
	}
	if flags.withTailscale && opts.TailscaleToken == "" {
		token, err := a.resolveOptionalServiceToken("Tailscale API token", "SERVERPRO_TAILSCALE_TOKEN", "TAILSCALE_API_TOKEN", true)
		if err != nil {
			return importsync.ImportOptions{}, err
		}
		opts.TailscaleToken = token
	}
	if flags.withCloudflare {
		token, err := a.resolveOptionalServiceToken("Cloudflare API token", "SERVERPRO_CLOUDFLARE_TOKEN", "CLOUDFLARE_API_TOKEN", true)
		if err != nil {
			return importsync.ImportOptions{}, err
		}
		opts.CloudflareToken = token
	}
	if flags.withTailscale {
		token := opts.TailscaleToken
		tailnet := flags.tailscaleTailnet
		if tailnet == "" {
			tailnet = "-"
		}
		opts.EnrichTailscale = func(ctx context.Context, candidate importsync.Candidate, cfg config.Config) (state.TailscaleState, error) {
			return importsync.MatchTailscaleDevice(ctx, tailscale.New(token, tailnet), candidate, cfg)
		}
	}
	if flags.withCloudflare {
		token := opts.CloudflareToken
		accountID := flags.cloudflareAccountID
		opts.EnrichCloudflare = func(ctx context.Context, candidate importsync.Candidate, cfg config.Config) (state.CloudflareState, error) {
			return importsync.MatchCloudflareTunnel(ctx, cloudflare.New(token, accountID), candidate, cfg)
		}
	}
	return opts, nil
}

func (a *app) resolveOptionalServiceToken(label, primaryEnv, fallbackEnv string, required bool) (string, error) {
	token := os.Getenv(primaryEnv)
	if token == "" {
		token = os.Getenv(fallbackEnv)
	}
	if token != "" {
		a.addRuntimeSecret(token)
		return token, nil
	}
	if !required {
		return "", nil
	}
	if a.nonInteractive {
		return "", fmt.Errorf("%s required for non-interactive import", primaryEnv)
	}
	token, err := a.promptSecret(label)
	if err != nil {
		return "", err
	}
	a.addRuntimeSecret(token)
	return token, nil
}
