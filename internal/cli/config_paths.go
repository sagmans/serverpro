package cli

import "github.com/assagman/serverpro/internal/config"

func (a *app) initialConfigPath(project, server string) string {
	if a.configPath != "" {
		return a.configPath
	}
	if project == "" {
		return ""
	}
	return config.ServerConfigPath(project, targetServer(server))
}

func (a *app) resolvedConfigPath(cfg config.Config) string {
	if a.configPath != "" {
		return a.configPath
	}
	return config.ServerConfigPath(cfg.Project, targetServer(cfg.Server))
}
