package cli

import (
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func (a *app) loadConfigFromRegistryTarget() (config.Config, string, bool, error) {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return config.Config{}, "", false, err
	}
	entry, ok := reg.Find(a.project, targetServer(a.server))
	if !ok || entry.ConfigPath == "" || !fileExists(config.Expand(entry.ConfigPath)) {
		return config.Config{}, "", false, nil
	}
	cfg, err := loadManagedServerConfig(entry.ConfigPath, configTarget{Project: entry.Project, Server: targetServer(entry.Server), ResourceNames: entry.ResourceNames})
	if err != nil {
		return cfg, "", true, err
	}
	return cfg, config.Expand(entry.StatePath), true, nil
}
