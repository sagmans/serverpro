package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	serverDeleteConfigLockSuffix    = ".lock"
	serverDeleteStateLockSuffix     = ".lock"
	serverDeleteOperationLockSuffix = ".operation.lock"
	serverDeleteImportMarkerSuffix  = ".import.json"
	serverDeleteLocalCleanupError   = "canonical local cleanup failed"
)

type serverDeleteLocalArtifacts struct {
	StatePath         string
	ConfigPath        string
	ConfigLockPath    string
	CredentialsPath   string
	CredentialsDir    string
	StateLockPath     string
	OperationLockPath string
	ImportMarkerPath  string
}

func canonicalServerDeleteLocalArtifacts(st state.State) serverDeleteLocalArtifacts {
	configPath := config.ServerConfigPath(st.Namespace, st.Server)
	credentialsPath := config.ServerCredentialsPath(st.Namespace, st.Server)
	statePath := config.ServerStatePath(st.Namespace, st.Server)
	return serverDeleteLocalArtifacts{
		StatePath:         statePath,
		ConfigPath:        configPath,
		ConfigLockPath:    configPath + serverDeleteConfigLockSuffix,
		CredentialsPath:   credentialsPath,
		CredentialsDir:    filepath.Dir(credentialsPath),
		StateLockPath:     statePath + serverDeleteStateLockSuffix,
		OperationLockPath: statePath + serverDeleteOperationLockSuffix,
		ImportMarkerPath:  statePath + serverDeleteImportMarkerSuffix,
	}
}

func (a serverDeleteLocalArtifacts) preview() []string {
	paths := []string{a.ConfigPath, a.ConfigLockPath, a.CredentialsPath, a.CredentialsDir, a.StatePath}
	for _, path := range []string{a.StateLockPath, a.OperationLockPath, a.ImportMarkerPath} {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func (a *app) executeServerDeleteLocal(authority serverDeleteAuthority, execution serverDeleteExecution) error {
	st := execution.Cleanup.State
	artifacts := canonicalServerDeleteLocalArtifacts(st)
	if err := removeCanonicalServerDeleteLocalArtifacts(artifacts); err != nil {
		return err
	}
	return state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		currentRegistry, exists := reg.Find(st.Namespace, targetServer(st.Server))
		if !sameServerDeleteRegistryAuthority(authority.Registry, true, currentRegistry, exists) {
			return fmt.Errorf("server destructive authority changed before registry cleanup; retained current registry entry")
		}
		reg.Remove(st.Namespace, targetServer(st.Server))
		return nil
	})
}

func removeCanonicalServerDeleteLocalArtifacts(artifacts serverDeleteLocalArtifacts) error {
	for _, path := range []string{
		artifacts.ConfigPath,
		artifacts.ConfigLockPath,
		artifacts.ImportMarkerPath,
		artifacts.StateLockPath,
		artifacts.OperationLockPath,
	} {
		if path == "" {
			continue
		}
		if err := privatefile.RemoveDurably(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: remove %s: %w", serverDeleteLocalCleanupError, path, err)
		}
	}
	if err := privatefile.RemoveDurably(artifacts.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: remove %s: %w", serverDeleteLocalCleanupError, artifacts.StatePath, err)
	}
	if err := privatefile.RemoveDurably(artifacts.CredentialsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: remove %s: %w", serverDeleteLocalCleanupError, artifacts.CredentialsPath, err)
	}
	if err := privatefile.RemoveDurably(artifacts.CredentialsDir); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
		return fmt.Errorf("%s: remove %s: %w", serverDeleteLocalCleanupError, artifacts.CredentialsDir, err)
	}
	return nil
}
