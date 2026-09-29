package credentials

import (
	"github.com/sagmans/serverpro/internal/config"
)

func Save(cfg config.Config, creds Set) error {
	return save(cfg, creds, true)
}

func SavePartial(cfg config.Config, creds Set) error {
	return save(cfg, creds, false)
}

// save publishes a caller-supplied set as the new stored state. Full sets use
// it after a complete read; partial changes belong in Update so the fields this
// caller never touched keep whatever a concurrent run wrote.
func save(cfg config.Config, creds Set, requireComplete bool) error {
	path, err := writeTargetPath(cfg)
	if err != nil {
		return err
	}
	creds.Namespace = cfg.Namespace
	creds.Server = cfg.Server
	if requireComplete {
		err = creds.ValidateForConfig(cfg)
	} else {
		err = creds.ValidateTarget(cfg.Namespace, cfg.Server)
	}
	if err != nil {
		return err
	}
	return withCredentialWriteLock(path, func() error {
		return writeCredentialSet(path, creds)
	})
}
