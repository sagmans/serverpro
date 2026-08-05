package credentials

import "github.com/sagmans/serverpro/internal/config"

func Load(cfg config.Config) (Set, error) {
	creds, err := LoadPartial(cfg)
	if err != nil {
		return creds, err
	}
	return creds, creds.ValidateForConfig(cfg)
}

func LoadPartial(cfg config.Config) (Set, error) {
	if err := validateConfigScope(cfg); err != nil {
		return Set{}, err
	}
	path := config.Expand(cfg.Credentials.JSONPath)
	if path == "" {
		return Set{Namespace: cfg.Namespace, Server: cfg.Server}, nil
	}
	path, err := safeCredentialPath(cfg.Namespace, cfg.Server, path, "load")
	if err != nil {
		return Set{}, err
	}
	if !fileExists(path) {
		return Set{Namespace: cfg.Namespace, Server: cfg.Server}, nil
	}
	creds, err := loadJSON(path)
	if err != nil {
		return creds, err
	}
	return creds, creds.ValidateTarget(cfg.Namespace, cfg.Server)
}
