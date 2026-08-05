package cli

import "github.com/assagman/serverpro/internal/config"

func (a *app) completeCloudflareConfig(cfg *config.Config, ask bool) error {
	if !cfg.Cloudflare.Tunnel.Enabled {
		return nil
	}
	if err := a.promptDefaultWhen(ask, "Cloudflare account ID", &cfg.Cloudflare.AccountID); err != nil {
		return err
	}
	return a.promptDefaultWhen(ask, "Cloudflare tunnel name", &cfg.Cloudflare.Tunnel.Name)
}
