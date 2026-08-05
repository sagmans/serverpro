package cli

import (
	"context"
	"errors"
	"time"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/redact"
	"github.com/spf13/cobra"
)

type serverBootstrapRow struct {
	Status    string `json:"status"`
	Action    string `json:"action"`
	DryRun    bool   `json:"dry_run,omitempty"`
	Namespace string `json:"namespace"`
	Server    string `json:"server"`
	Target    string `json:"target"`
	User      string `json:"user,omitempty"`
	Host      string `json:"host,omitempty"`
}

func (a *app) serverBootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap NAME [all|git|docker|mise|node|pi]",
		Short: "bootstrap tools on an existing managed server",
		Long: "Bootstrap managed host tools on an existing server. The all target installs " +
			bootstraptools.DefaultToolsetDescription() +
			". Pi and gh authentication remain operator-owned; serverpro does not configure or store their credentials.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a.server = args[0]
			targetArg := ""
			if len(args) == 2 {
				targetArg = args[1]
			}
			target, err := bootstraptools.ParseTarget(targetArg)
			if err != nil {
				return err
			}
			cfg, _, st, err := a.loadConfigAndStateForServer(args[0])
			if err != nil {
				return err
			}
			if st.Tailscale.Name == "" {
				return errors.New("tailscale host missing from state; run serverpro server create or server doctor")
			}
			if a.dryRun {
				row := serverBootstrapRow{Status: "planned", Action: "bootstrap", DryRun: true, Namespace: cfg.Project, Server: cfg.Server, Target: string(target), User: cfg.Admin.Username, Host: st.Tailscale.Name}
				return writeJSON(a.stdout, row)
			}
			sudoPassword, err := a.resolveSudoPassword(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()
			if err := a.bootstrapTools(ctx, cfg, st, sudoPassword, target); err != nil {
				return redact.New(a.runtimeSecrets...).Error(err)
			}
			if target.IncludesGit() {
				if err := a.maybeSetupGitDeployAccess(ctx, cfg, st, sudoPassword); err != nil {
					return redact.New(a.runtimeSecrets...).Error(err)
				}
			}
			row := serverBootstrapRow{Status: "complete", Action: "bootstrap", Namespace: cfg.Project, Server: cfg.Server, Target: string(target), User: cfg.Admin.Username, Host: st.Tailscale.Name}
			return writeJSON(a.stdout, row)
		},
	}
}
