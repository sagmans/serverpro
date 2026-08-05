package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

type configSourceSnapshot struct {
	path   string
	exists bool
	body   []byte
}

func (a *app) prepareConfigForCreate(ctx context.Context, namespace, server string) (config.Config, string, func(), error) {
	cfg, stPath, source, err := a.buildPreparedConfig(namespace, server)
	if err != nil {
		return cfg, "", nil, err
	}
	unlock, err := state.LockServerWorkflow(ctx, config.RegistryPath(), cfg.Namespace, config.Expand(stPath))
	if err != nil {
		return cfg, "", nil, err
	}
	if err := a.publishPreparedConfig(cfg, source); err != nil {
		unlock()
		return cfg, "", nil, err
	}
	return cfg, stPath, unlock, nil
}

func (a *app) buildPreparedConfig(namespace, server string) (config.Config, string, configSourceSnapshot, error) {
	sourcePath := a.initialConfigPath(namespace, server)
	source, err := snapshotConfigSource(sourcePath)
	if err != nil {
		return config.Config{}, "", source, err
	}
	cfg, stPath, err := a.prepareConfigValuesFromSource(namespace, server, source)
	if err != nil {
		return cfg, "", source, err
	}
	if sourcePath != "" {
		return cfg, stPath, source, nil
	}
	source, err = snapshotConfigSource(a.resolvedConfigPath(cfg))
	if err != nil {
		return cfg, "", source, err
	}
	if source.exists {
		return cfg, "", source, fmt.Errorf("server config appeared while preparing create; retry create")
	}
	return cfg, stPath, source, nil
}

func (a *app) publishPreparedConfig(cfg config.Config, source configSourceSnapshot) error {
	if err := source.requireUnchanged(); err != nil {
		return err
	}
	destination := config.Expand(a.resolvedConfigPath(cfg))
	if source.path != destination {
		return fmt.Errorf("server config target changed while preparing create; retry create")
	}
	if err := config.SaveIfUnchanged(destination, cfg, source.body, source.exists); err != nil {
		if errors.Is(err, config.ErrSourceChanged) {
			return fmt.Errorf("server config changed while awaiting create lock; retry create")
		}
		return err
	}
	return nil
}

func (a *app) prepareConfigValuesFromSource(namespace, server string, source configSourceSnapshot) (config.Config, string, error) {
	cfg, exists, err := a.loadOrCreateConfigFromSource(namespace, server, source)
	if err != nil {
		return cfg, "", err
	}
	return a.completePreparedConfig(cfg, exists)
}

func (a *app) completePreparedConfig(cfg config.Config, exists bool) (config.Config, string, error) {
	a.applyCreateOverrides(&cfg)
	if err := a.completeConfig(&cfg, exists); err != nil {
		return cfg, "", err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, "", err
	}
	stPath := a.statePath
	if stPath == "" {
		var err error
		stPath, err = a.resolveStatePath(cfg)
		if err != nil {
			return cfg, "", err
		}
	}
	return cfg, stPath, nil
}

func snapshotConfigSource(path string) (configSourceSnapshot, error) {
	snapshot := configSourceSnapshot{path: config.Expand(path)}
	if snapshot.path == "" {
		return snapshot, nil
	}
	body, err := os.ReadFile(snapshot.path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	snapshot.exists = true
	snapshot.body = body
	return snapshot, nil
}

func (snapshot configSourceSnapshot) requireUnchanged() error {
	current, err := snapshotConfigSource(snapshot.path)
	if err != nil {
		return err
	}
	if current.path != snapshot.path || current.exists != snapshot.exists || !bytes.Equal(current.body, snapshot.body) {
		return fmt.Errorf("server config changed while awaiting create lock; retry create")
	}
	return nil
}

func (a *app) loadOrCreateConfigFromSource(namespace, server string, source configSourceSnapshot) (config.Config, bool, error) {
	if source.exists {
		cfg, err := config.LoadPartialBytes(source.body)
		if err != nil {
			return cfg, true, err
		}
		applyConfigTargetOverrides(&cfg, namespace, server)
		config.ApplyDefaults(&cfg)
		return cfg, true, nil
	}
	return a.newConfig(namespace, server)
}

func (a *app) newConfig(namespace, server string) (config.Config, bool, error) {
	if a.nonInteractive && namespace == "" {
		return config.Config{}, false, fmt.Errorf("managed config missing; pass --namespace/-n or --config")
	}
	if namespace == "" {
		var err error
		namespace, err = a.promptDefault("namespace", "example")
		if err != nil {
			return config.Config{}, false, err
		}
	}
	return config.ExampleServer(namespace, server), false, nil
}
