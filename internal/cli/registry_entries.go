package cli

import (
	"path/filepath"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func (a *app) upsertRegistryEntry(cfg config.Config, statePath string) error {
	cfgPath := config.Expand(a.resolvedConfigPath(cfg))
	if abs, err := filepath.Abs(cfgPath); err == nil {
		cfgPath = abs
	}
	return state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{
			Namespace:       cfg.Namespace,
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
