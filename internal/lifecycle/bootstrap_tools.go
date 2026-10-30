package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/network"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/state"
)

func BootstrapTools(ctx context.Context, r remote.Runner, cfg config.Config, st state.State, target bootstraptools.Target) error {
	if r == nil || st.Tailscale.Name == "" {
		return fmt.Errorf("remote host unavailable for bootstrap")
	}
	script, err := bootstraptools.InstallScriptForUserTarget(cfg.Admin.Username, target)
	if err != nil {
		return err
	}
	if _, err := remote.WithTimeout(r, 20*time.Minute).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, script); err != nil {
		return fmt.Errorf("bootstrap %s: %w", target, err)
	}
	return applyEgressLockdown(ctx, r, cfg, st)
}

// applyEgressLockdown runs after the tools in create (only the OpenSSH-off
// step follows) and last in server bootstrap. Bootstrap is the recovery after
// a create whose tool phase failed, and that create never reached the
// lockdown, so bootstrap must converge egress too or the host stays open with
// only the tools' rules.
func applyEgressLockdown(ctx context.Context, r remote.Runner, cfg config.Config, st state.State) error {
	if !cfg.Network.Egress.PhaseLockdownAfterBootstrap {
		return nil
	}
	if _, err := r.Run(ctx, cfg.Admin.Username, st.Tailscale.Name, network.LockdownScript(cfg)); err != nil {
		return fmt.Errorf("egress lockdown: %w", err)
	}
	return nil
}
