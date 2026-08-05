package cli

import "github.com/sagmans/serverpro/internal/config"

func (a *app) completeAdminConfig(cfg *config.Config, ask bool) error {
	return a.promptDefaultWhen(ask, "admin username", &cfg.Admin.Username)
}
