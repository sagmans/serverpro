package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/importsync"
	"github.com/spf13/cobra"
)

type importFlags struct {
	providerID          string
	discoverServer      string
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
	cmd := withScopedFlags(&cobra.Command{
		Use:   "discover",
		Short: "list provider servers labeled for serverpro",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runServerDiscover(cmd.Context(), flags)
		},
	}, "namespace", "provider", "non-interactive")
	cmd.Flags().StringVar(&flags.providerID, "provider-id", "", "filter by provider resource id")
	cmd.Flags().StringVar(&flags.discoverServer, "server", "", "filter by server name")
	cmd.Flags().BoolVar(&flags.includeUnmanaged, "include-unmanaged", false, "include servers without serverpro ownership labels")
	return cmd
}

func (a *app) serverImportCmd() *cobra.Command {
	flags := &importFlags{}
	cmd := withScopedFlags(&cobra.Command{
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
	}, "namespace", "provider", "all", "non-interactive", "dry-run", "yes")
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
	provider, account, err := a.importProviderAccount("serverpro server discover")
	if err != nil {
		return err
	}
	candidates, err := importsync.Discover(ctx, provider, account, importsync.DiscoverFilter{
		Namespace:        a.project,
		Server:           flags.discoverServer,
		ProviderID:       flags.providerID,
		IncludeUnmanaged: flags.includeUnmanaged,
	})
	if err != nil {
		return err
	}
	return writeJSON(a.stdout, candidates)
}

func (a *app) runServerImport(ctx context.Context, name string, flags *importFlags) error {
	if err := validateServerImportRequest(name, flags, a.all); err != nil {
		return err
	}
	candidates, providerToken, err := a.selectImportCandidates(ctx, name, flags)
	if err != nil {
		return err
	}
	if !a.dryRun {
		if err := a.confirmImport(len(candidates)); err != nil {
			return err
		}
	}
	if err := a.resolveImportAdminUser(flags); err != nil {
		return err
	}
	options, err := a.buildImportOptions(candidates, providerToken, flags)
	if err != nil {
		return err
	}
	results, err := importsync.ImportAll(ctx, options)
	if err != nil {
		return err
	}
	return writeJSON(a.stdout, results)
}

func validateServerImportRequest(name string, flags *importFlags, all bool) error {
	if name == "" && flags.providerID == "" && !all {
		return fmt.Errorf("import requires NAME, --provider-id, or --all")
	}
	if name != "" && all {
		return fmt.Errorf("import NAME and --all are mutually exclusive")
	}
	if flags.providerID != "" && all {
		return fmt.Errorf("--provider-id and --all are mutually exclusive")
	}
	if flags.withCloudflare && flags.cloudflareAccountID == "" {
		return fmt.Errorf("--cloudflare-account-id required with --with-cloudflare")
	}
	return nil
}

func (a *app) selectImportCandidates(ctx context.Context, name string, flags *importFlags) ([]importsync.Candidate, string, error) {
	provider, account, err := a.importProviderAccount("serverpro server import")
	if err != nil {
		return nil, "", err
	}
	candidates, err := importsync.Discover(ctx, provider, account, importsync.DiscoverFilter{
		Namespace:  a.project,
		Server:     name,
		ProviderID: flags.providerID,
	})
	if err != nil {
		return nil, "", err
	}
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("no managed servers matched import criteria")
	}
	if name != "" && a.project == "" && len(candidates) > 1 {
		return nil, "", fmt.Errorf("server %q is ambiguous across namespaces; pass --namespace", name)
	}
	return candidates, account.Token, nil
}

func (a *app) importProviderAccount(commandPath string) (compute.Provider, compute.Account, error) {
	if a.provider == "" {
		return nil, compute.Account{}, requiredFlagError(commandPath, "provider", "p")
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
	wireImportEnrichers(&opts)
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
