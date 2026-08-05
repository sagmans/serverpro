package lifecycle

import (
	"context"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func cloudflareTunnelConfigured(cfg config.Config) bool {
	// Account credentials can exist without consent to expose the server publicly.
	return cfg.Cloudflare.Tunnel.Enabled
}

func ensureCloudflareTunnel(ctx context.Context, st *state.State, stPath string, cfg config.Config, c CloudflareClient) error {
	if !cloudflareTunnelConfigured(cfg) {
		return nil
	}
	if st.Cloudflare.TunnelID != "" {
		return nil
	}
	tun, err := c.CreateTunnel(ctx, cfg.Cloudflare.Tunnel.Name)
	if err != nil {
		return err
	}
	st.Cloudflare = state.CloudflareState{TunnelID: tun.ID, Name: tun.Name}
	return state.Save(stPath, *st)
}
