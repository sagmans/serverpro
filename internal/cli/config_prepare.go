package cli

import (
	"fmt"

	"github.com/assagman/serverpro/internal/config"
)

func (a *app) prepareConfig(project, server string) (config.Config, string, error) {
	cfg, exists, err := a.loadOrCreateConfig(project, server)
	if err != nil {
		return cfg, "", err
	}
	a.applyCreateOverrides(&cfg)
	if err := a.completeConfig(&cfg, exists); err != nil {
		return cfg, "", err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, "", err
	}
	if a.provider != "" {
		provider, err := a.resolveProvider(a.provider)
		if err != nil {
			return cfg, "", err
		}
		if err := validateCreateImageReference(provider, cfg.Compute.Image); err != nil {
			return cfg, "", err
		}
	}
	if err := config.Save(a.resolvedConfigPath(cfg), cfg); err != nil {
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

func (a *app) loadOrCreateConfig(project, server string) (config.Config, bool, error) {
	if path := a.initialConfigPath(project, server); path != "" && fileExists(config.Expand(path)) {
		cfg, err := config.LoadPartial(path)
		if err != nil {
			return cfg, true, err
		}
		applyConfigTargetOverrides(&cfg, project, server)
		config.ApplyDefaults(&cfg)
		return cfg, true, nil
	}
	if a.nonInteractive && project == "" {
		return config.Config{}, false, fmt.Errorf("managed config missing; pass --namespace/-n or --config")
	}
	if project == "" {
		var err error
		project, err = a.promptDefault("namespace", "example")
		if err != nil {
			return config.Config{}, false, err
		}
	}
	return config.ExampleServer(project, server), false, nil
}
