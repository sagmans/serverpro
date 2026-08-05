package cli

import (
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func (a *app) loadConfigForPreview() (config.Config, error) {
	if a.configPath == "" && a.project != "" {
		if cfg, _, ok, err := a.loadConfigFromRegistryTarget(); err != nil || ok {
			return cfg, err
		}
	}
	path := a.initialConfigPath(a.project, a.server)
	if path == "" || !fileExists(config.Expand(path)) {
		return config.Config{}, fmt.Errorf("managed config missing; run serverpro server create <server> -n <namespace> or pass --config")
	}
	cfg, err := loadManagedServerConfig(path, configTarget{Namespace: a.project, Server: a.server})
	return cfg, err
}

func (a *app) loadConfigWithState() (config.Config, string, error) {
	if a.configPath == "" && a.statePath == "" && a.project != "" {
		if cfg, stPath, ok, err := a.loadConfigFromRegistryTarget(); err != nil || ok {
			return cfg, stPath, err
		}
	}
	path := a.initialConfigPath(a.project, a.server)
	if path == "" || !fileExists(config.Expand(path)) {
		return config.Config{}, "", fmt.Errorf("managed config missing; run serverpro server create <server> -n <namespace> or pass --config")
	}
	cfg, err := loadManagedServerConfig(path, configTarget{Namespace: a.project, Server: a.server})
	if err != nil {
		return cfg, "", err
	}
	stPath := a.statePath
	if stPath == "" {
		stPath, err = a.resolveStatePath(cfg)
		if err != nil {
			return cfg, "", err
		}
	}
	return cfg, stPath, nil
}

func (a *app) loadConfigAndStateForServer(name string) (config.Config, string, state.State, error) {
	a.server = name
	if a.project == "" && a.configPath == "" && a.statePath == "" {
		match, err := a.resolveServerStateMatch(name)
		if err != nil {
			return config.Config{}, "", state.State{}, err
		}
		a.project = match.State.Namespace
		if a.provider == "" {
			a.provider = match.State.Compute.Provider
		}
		cfgPath := match.ConfigPath
		if cfgPath == "" {
			cfgPath = config.ServerConfigPath(match.State.Namespace, name)
		}
		if !fileExists(config.Expand(cfgPath)) {
			return config.Config{}, "", state.State{}, fmt.Errorf("managed config missing; run serverpro server create %s -n %s or pass --config", name, match.State.Namespace)
		}
		cfg, err := loadManagedServerConfig(cfgPath, configTarget{Namespace: match.State.Namespace, Server: name, ResourceNames: match.ResourceNames})
		if err != nil {
			return cfg, "", state.State{}, err
		}
		if err := validateStateTarget(cfg, match.State); err != nil {
			return cfg, "", state.State{}, err
		}
		return cfg, match.StatePath, match.State, nil
	}
	cfg, stPath, err := a.loadConfigWithState()
	if err != nil {
		return cfg, "", state.State{}, err
	}
	st, err := loadState(stPath, cfg.Namespace, cfg.Server)
	if err != nil {
		return cfg, "", state.State{}, err
	}
	if err := validateStateTarget(cfg, st); err != nil {
		return cfg, "", state.State{}, err
	}
	return cfg, stPath, st, nil
}

type configTarget struct {
	Namespace     string
	Server        string
	ResourceNames state.RegistryResourceNames
}

func loadManagedServerConfig(path string, target configTarget) (config.Config, error) {
	cfg, err := config.LoadPartial(path)
	if err != nil {
		return cfg, err
	}
	applyManagedConfigTarget(&cfg, target)
	config.ApplyDefaults(&cfg)
	return cfg, cfg.Validate()
}

func applyConfigTargetOverrides(cfg *config.Config, namespace, server string) {
	applyManagedConfigTarget(cfg, configTarget{Namespace: namespace, Server: server})
}

func applyManagedConfigTarget(cfg *config.Config, target configTarget) {
	if target.Namespace != "" {
		cfg.Namespace = target.Namespace
	}
	if target.Server != "" {
		cfg.Server = target.Server
	}
	if cfg.Namespace != "" && cfg.Server != "" {
		cfg.Credentials.JSONPath = config.ServerCredentialsPath(cfg.Namespace, cfg.Server)
		cfg.Compute.Name = config.ServerResourceName(cfg.Namespace, cfg.Server)
		cfg.Cloudflare.Tunnel.Name = cfg.Compute.Name
	}
	if target.ResourceNames.ComputeServer != "" {
		cfg.Compute.Name = target.ResourceNames.ComputeServer
	}
	if target.ResourceNames.CloudflareTunnel != "" {
		cfg.Cloudflare.Tunnel.Name = target.ResourceNames.CloudflareTunnel
	}
}
