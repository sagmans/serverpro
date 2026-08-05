package cli

import "github.com/assagman/serverpro/internal/config"

func (a *app) completeAdminConfig(cfg *config.Config, ask bool) error {
	return a.promptDefaultWhen(ask, "admin username", &cfg.Admin.Username)
}
