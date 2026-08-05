package credentials

import (
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
)

func validateConfigScope(cfg config.Config) error {
	if cfg.Project == "" {
		return fmt.Errorf("credentials namespace required")
	}
	if cfg.Server == "" {
		return fmt.Errorf("credentials server required")
	}
	if !config.CredentialsPathScopedToServer(cfg.Credentials.JSONPath, cfg.Project, cfg.Server) {
		return fmt.Errorf("credentials.json_path must be scoped to server %q in namespace %q", cfg.Server, cfg.Project)
	}
	return nil
}

func credentialRoot() string {
	return config.Expand("~/.config/serverpro")
}

func safeCredentialPath(project, server, path, action string) (string, error) {
	if !config.CredentialsPathScopedToServer(path, project, server) {
		return "", fmt.Errorf("credentials.json_path must be scoped to server %q in namespace %q", server, project)
	}
	absPath, err := privatefile.ResolveUnderRoot(path, credentialRoot(), "credentials")
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkInCredentialAbsPath(absPath, action); err != nil {
		return "", err
	}
	return absPath, nil
}

func rejectSymlinkInCredentialAbsPath(absPath, action string) error {
	return privatefile.RejectSymlinkPath(absPath, config.Expand("~"), "credentials", action)
}
