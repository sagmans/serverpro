package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/state"
)

// cloudflareTunnelRollbackTimeout bounds compensating deletion without letting
// an expired provisioning context strand a just-created tunnel.
const cloudflareTunnelRollbackTimeout = 10 * time.Second

func cloudflareTunnelConfigured(cfg config.Config) bool {
	// Account credentials can exist without consent to expose the server publicly.
	return cfg.Cloudflare.Tunnel.Enabled
}

func ensureCloudflareTunnel(ctx context.Context, st *state.State, stPath string, cfg config.Config, c CloudflareClient, save provisionStateSaver) error {
	if !cloudflareTunnelConfigured(cfg) || st.Cloudflare.TunnelID != "" {
		return nil
	}
	tunnels, err := c.ListTunnels(ctx)
	if err != nil {
		return fmt.Errorf("cloudflare tunnel list failed: %w", err)
	}
	tun, found, err := ingress.MatchTunnelByName(tunnels, cfg.Cloudflare.Tunnel.Name)
	if err != nil {
		return err
	}
	fresh := !found
	provenance := state.CloudflareTunnelAdopted
	if fresh {
		tun, err = c.CreateTunnel(ctx, cfg.Cloudflare.Tunnel.Name)
		if err != nil {
			return err
		}
		provenance = state.CloudflareTunnelCreated
	}
	st.Cloudflare = state.CloudflareState{TunnelID: tun.ID, Name: tun.Name, Provenance: provenance}
	if err := save(stPath, *st); err != nil {
		checkpointErr := fmt.Errorf("cloudflare tunnel checkpoint failed: %w", err)
		if !fresh {
			return checkpointErr
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), cloudflareTunnelRollbackTimeout)
		defer cancel()
		if rollbackErr := c.DeleteTunnel(rollbackCtx, tun.ID); rollbackErr != nil && !httpjson.IsStatus(rollbackErr, http.StatusNotFound) {
			return errors.Join(checkpointErr, fmt.Errorf("cloudflare tunnel rollback failed: %w", rollbackErr))
		}
		return checkpointErr
	}
	return nil
}
