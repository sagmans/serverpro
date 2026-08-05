package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/spf13/cobra"
)

type app struct {
	configPath        string
	statePath         string
	project           string
	server            string
	provider          string
	all               bool
	nonInteractive    bool
	dryRun            bool
	timeout           string
	timeoutCancel     context.CancelFunc
	yes               bool
	doctorFix         bool
	create            createOverrides
	stdin             io.Reader
	stdout            io.Writer
	stderr            io.Writer
	reader            *bufio.Reader
	sudoPasswords     map[string]string
	runtimeSecrets    []string
	ephemeralAccounts map[compute.ProviderName]compute.Account
	providers         *compute.Registry
	ingressAdapters   map[ingress.Type]ingress.Adapter
	selectChoice      func(string, string, []choice) (string, bool, error)
	selectServerMatch func([]string) (string, bool, error)
	now               func() time.Time
	services          serviceHooks
}

const (
	developmentVersion          = "dev"
	developmentBuildInfoVersion = "(devel)"
)

var Version = developmentVersion

func New() *cobra.Command {
	return newRoot(&app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr})
}

func newRoot(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "serverpro",
		Short:         "Secure multi-provider server provisioner",
		Version:       resolvedVersion(),
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.prepareRootCommand(cmd)
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			a.cleanupRootCommand()
		},
	}
	cmd.SetVersionTemplate("serverpro version {{.Version}}\n")
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.PersistentFlags().StringVar(&a.configPath, "config", "", "config path override (default: managed config store)")
	cmd.PersistentFlags().StringVar(&a.statePath, "state", "", "state path")
	cmd.PersistentFlags().StringVarP(&a.project, "namespace", "n", "", "serverpro namespace")
	cmd.PersistentFlags().StringVarP(&a.provider, "provider", "p", "", "compute provider")
	cmd.PersistentFlags().BoolVarP(&a.all, "all", "A", false, "show all matching resources")
	cmd.PersistentFlags().BoolVar(&a.nonInteractive, "non-interactive", false, "disable prompts and fail on missing input")
	cmd.PersistentFlags().BoolVar(&a.dryRun, "dry-run", false, "preview supported commands without mutations")
	cmd.PersistentFlags().BoolVarP(&a.yes, "yes", "y", false, "assume yes for confirmations")
	cmd.PersistentFlags().StringVar(&a.timeout, "timeout", "", "operation timeout")
	installScopedFlagHelp(cmd)
	cmd.AddCommand(a.namespaceCmd(), a.serverCmd(), a.providerCmd(), a.catalogCmd(), a.ingressCmd(), a.tailnetCmd(), a.globalDoctorCmd())
	return cmd
}

func resolvedVersion() string {
	info, ok := debug.ReadBuildInfo()
	return versionFromBuildInfo(Version, info, ok)
}

func versionFromBuildInfo(configured string, info *debug.BuildInfo, ok bool) string {
	// Linker-injected releases remain authoritative; build info only fixes go-install defaults.
	if configured != developmentVersion || !ok || info == nil || info.Main.Version == "" || info.Main.Version == developmentBuildInfoVersion {
		return configured
	}
	return info.Main.Version
}

func (a *app) prepareRootCommand(cmd *cobra.Command) error {
	a.stdin = cmd.InOrStdin()
	a.stdout = cmd.OutOrStdout()
	a.stderr = cmd.ErrOrStderr()
	if err := validateScopedPersistentFlags(cmd); err != nil {
		return err
	}
	if a.timeout == "" {
		return nil
	}
	duration, err := time.ParseDuration(a.timeout)
	if err != nil {
		return err
	}
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, duration)
	a.timeoutCancel = cancel
	cmd.SetContext(ctx)
	return nil
}

func validateScopedPersistentFlags(cmd *cobra.Command) error {
	path := cmd.CommandPath()
	allowed := scopedPersistentFlags[path]
	for _, flag := range scopedPersistentFlagNames {
		if commandFlagChanged(cmd, flag) && !allowed[flag] {
			return fmt.Errorf("--%s is not supported by %q", flag, path)
		}
	}
	return nil
}

func commandFlagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Flag(name)
	return flag != nil && flag.Changed
}

func installScopedFlagHelp(root *cobra.Command) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		restore := hideUnsupportedScopedFlags(cmd)
		defer restore()
		defaultHelp(cmd, args)
	})
}

func hideUnsupportedScopedFlags(cmd *cobra.Command) func() {
	allowed := scopedPersistentFlags[cmd.CommandPath()]
	flags := cmd.Root().PersistentFlags()
	previous := make(map[string]bool, len(scopedPersistentFlagNames))
	for _, name := range scopedPersistentFlagNames {
		flag := flags.Lookup(name)
		if flag == nil {
			continue
		}
		previous[name] = flag.Hidden
		flag.Hidden = !allowed[name]
	}
	return func() {
		for name, hidden := range previous {
			flags.Lookup(name).Hidden = hidden
		}
	}
}

var scopedPersistentFlagNames = []string{"config", "state", "namespace", "provider", "all", "non-interactive", "dry-run", "yes"}

var scopedPersistentFlags = map[string]map[string]bool{
	"serverpro namespace create":  {"dry-run": true},
	"serverpro namespace delete":  {"dry-run": true, "non-interactive": true, "yes": true},
	"serverpro server create":     {"config": true, "state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true, "yes": true},
	"serverpro server list":       {"namespace": true, "provider": true},
	"serverpro server status":     {"state": true, "namespace": true, "provider": true, "all": true, "non-interactive": true},
	"serverpro server doctor":     {"config": true, "state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true},
	"serverpro server ssh":        {"state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true},
	"serverpro server delete":     {"state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true, "yes": true},
	"serverpro server start":      {"state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true, "yes": true},
	"serverpro server stop":       {"state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true, "yes": true},
	"serverpro server restart":    {"state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true, "yes": true},
	"serverpro server bootstrap":  {"config": true, "state": true, "namespace": true, "provider": true, "non-interactive": true, "dry-run": true},
	"serverpro server discover":   {"namespace": true, "provider": true, "non-interactive": true},
	"serverpro server import":     {"namespace": true, "provider": true, "all": true, "non-interactive": true, "dry-run": true, "yes": true},
	"serverpro provider doctor":   {"non-interactive": true},
	"serverpro catalog locations": {"provider": true, "non-interactive": true},
	"serverpro catalog sizes":     {"provider": true, "non-interactive": true},
	"serverpro catalog images":    {"provider": true, "non-interactive": true},
	"serverpro ingress add":       {"state": true, "namespace": true, "non-interactive": true, "dry-run": true},
	"serverpro ingress list":      {"state": true, "namespace": true, "non-interactive": true},
	"serverpro ingress remove":    {"state": true, "namespace": true, "non-interactive": true, "dry-run": true},
	"serverpro tailnet reconcile": {"non-interactive": true, "dry-run": true, "yes": true},
}

func (a *app) cleanupRootCommand() {
	if a.timeoutCancel != nil {
		a.timeoutCancel()
		a.timeoutCancel = nil
	}
}
