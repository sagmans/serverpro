package cli

import (
	"path/filepath"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func (a *app) upsertRegistryEntry(cfg config.Config, statePath string) error {
	cfgPath := config.Expand(a.resolvedConfigPath(cfg))
	if abs, err := filepath.Abs(cfgPath); err == nil {
		cfgPath = abs
	}
	return state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{
			Project:         cfg.Project,
			Server:          targetServer(cfg.Server),
			StatePath:       statePath,
			ConfigPath:      cfgPath,
			CredentialsPath: cfg.Credentials.JSONPath,
			ResourceNames:   state.RegistryResourceNames{ComputeServer: cfg.Compute.Name, CloudflareTunnel: cfg.Cloudflare.Tunnel.Name},
			Labels:          cfg.Compute.Labels,
		})
		return nil
	})
}
