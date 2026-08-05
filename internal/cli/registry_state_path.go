package cli

import (
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func (a *app) resolveStatePath(cfg config.Config) (string, error) {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return "", err
	}
	server := targetServer(cfg.Server)
	if entry, ok := reg.Find(cfg.Project, server); ok && entry.StatePath != "" {
		return config.Expand(entry.StatePath), nil
	}
	return config.ServerStatePath(cfg.Project, server), nil
}
