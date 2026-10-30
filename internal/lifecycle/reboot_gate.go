package lifecycle

import (
	"context"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/state"
)

// installRebootGate runs after the bootstrap, so the timer goes on only once
// the host carries its workloads' tools, and before OpenSSH goes off, which
// stays the last step. With the gate off in config it does nothing. Without
// a remote host it fails closed, as the egress lockdown does: config
// promises the gate, and a create that never installed it must not report
// success.
func installRebootGate(ctx context.Context, r remote.Runner, cfg config.Config, st state.State) error {
	if !cfg.Reboot.Enabled {
		return nil
	}
	if r == nil || st.Tailscale.Name == "" {
		return fmt.Errorf("reboot gate: no remote host to install the gated reboot on")
	}
	script, err := rebootgate.InstallScript(cfg.Reboot)
	if err != nil {
		return fmt.Errorf("reboot gate: %w", err)
	}
	if _, err := r.Run(ctx, cfg.Admin.Username, st.Tailscale.Name, script); err != nil {
		return fmt.Errorf("reboot gate: %w", err)
	}
	return nil
}
