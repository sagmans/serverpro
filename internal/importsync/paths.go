package importsync

import "github.com/sagmans/serverpro/internal/config"

func defaultRegistryPath() string {
	return config.RegistryPath()
}

func defaultServerConfigPath(namespace, server string) string {
	return config.ServerConfigPath(namespace, server)
}

func defaultServerStatePath(namespace, server string) string {
	return config.ServerStatePath(namespace, server)
}
