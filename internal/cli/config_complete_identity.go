package cli

import "github.com/sagmans/serverpro/internal/config"

func (a *app) completeConfigIdentity(cfg *config.Config, ask bool) error {
	if ask && cfg.Namespace == "" {
		if err := a.promptDefaultWhen(true, "namespace (local resource group)", &cfg.Namespace); err != nil {
			return err
		}
	}
	config.ApplyDefaults(cfg)
	if cfg.Server == "" {
		cfg.Server = config.DefaultServer()
	}
	if ask {
		if err := a.promptDefaultWhen(true, "server (logical name)", &cfg.Server); err != nil {
			return err
		}
		config.ApplyDefaults(cfg)
	}
	return nil
}
