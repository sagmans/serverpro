package cli

import "github.com/sagmans/serverpro/internal/config"

func (a *app) initialConfigPath(namespace, server string) string {
	if a.configPath != "" {
		return a.configPath
	}
	if namespace == "" {
		return ""
	}
	return config.ServerConfigPath(namespace, targetServer(server))
}

func (a *app) resolvedConfigPath(cfg config.Config) string {
	if a.configPath != "" {
		return a.configPath
	}
	return config.ServerConfigPath(cfg.Namespace, targetServer(cfg.Server))
}
