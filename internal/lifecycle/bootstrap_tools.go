package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/remote"
	"github.com/assagman/serverpro/internal/state"
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
	return nil
}
