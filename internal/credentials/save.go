package credentials

import (
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
)

func Save(cfg config.Config, creds Set) error {
	if err := validateConfigScope(cfg); err != nil {
		return err
	}
	creds.Namespace = cfg.Namespace
	creds.Server = cfg.Server
	if err := creds.ValidateForConfig(cfg); err != nil {
		return err
	}
	path := config.Expand(cfg.Credentials.JSONPath)
	if path == "" {
		return fmt.Errorf("credentials JSON path required")
	}
	path, err := safeCredentialPath(cfg.Namespace, cfg.Server, path, "save")
	if err != nil {
		return err
	}
	if err := rejectSymlinkInCredentialAbsPath(path, "save"); err != nil {
		return err
	}
	return privatefile.AtomicWriteJSON(path, creds, privatefile.WriteOptions{TempPattern: ".credentials-*.tmp", Sync: true, BeforeRename: func() error {
		return rejectSymlinkInCredentialAbsPath(path, "save")
	}})
}
