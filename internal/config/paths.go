package config

import (
	"os"
	"path/filepath"

	"github.com/sagmans/serverpro/internal/servername"
)

func RegistryPath() string {
	return Expand("~/.local/state/serverpro/registry.json")
}

func LocalArtifactGuardPath() string {
	return Expand("~/.local/state/serverpro/.local-artifacts.lock")
}

func NamespaceStateDir(namespace string) string {
	return Expand(filepath.Join("~/.local/state/serverpro/namespaces", namespace))
}

func ServerStatePath(namespace, server string) string {
	return filepath.Join(NamespaceStateDir(namespace), "servers", servername.Normalize(server)+".json")
}

func NamespaceConfigDir(namespace string) string {
	return Expand(filepath.Join("~/.config/serverpro/namespaces", namespace))
}

func ServerCredentialsPath(namespace, server string) string {
	if namespace == "" {
		return defaultCredentialsJSONPath
	}
	return filepath.Join(NamespaceConfigDir(namespace), "servers", servername.Normalize(server), "credentials.json")
}

func ServerConfigPath(namespace, server string) string {
	return filepath.Join(NamespaceConfigDir(namespace), "servers", servername.Normalize(server)+".yaml")
}

func Expand(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if len(path) == 1 {
		return home
	}
	if path[1] == '/' {
		return filepath.Join(home, path[2:])
	}
	return path
}
