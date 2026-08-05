package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/sagmans/serverpro/internal/privatefile"
	"gopkg.in/yaml.v3"
)

func Load(path string) (Config, error) {
	cfg, err := LoadPartial(path)
	if err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

type configFile struct {
	Config  `yaml:",inline"`
	Project string `yaml:"project,omitempty"`
}

const configLockSuffix = ".lock"

// ErrSourceChanged reports that conditional publication lost source authority.
var ErrSourceChanged = errors.New("config source changed")

func LoadPartial(path string) (Config, error) {
	body, err := os.ReadFile(Expand(path))
	if err != nil {
		return Config{}, err
	}
	return LoadPartialBytes(body)
}

// LoadPartialBytes binds parsing to bytes already approved by the caller.
func LoadPartialBytes(body []byte) (Config, error) {
	d := Default()
	// WHY: decoding onto this one true-by-default safety bit distinguishes an
	// omitted field from an explicit false without inventing unrelated defaults
	// such as the admin username or catalog selections.
	file := configFile{Config: Config{Network: Network{Egress: Egress{PhaseLockdownAfterBootstrap: d.Network.Egress.PhaseLockdownAfterBootstrap}}}}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return file.Config, err
	}
	cfg := file.Config
	if err := validateNamespaceIdentity(cfg.Namespace, file.Project); err != nil {
		return cfg, err
	}
	if cfg.Namespace == "" {
		cfg.Namespace = file.Project
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func Save(path string, cfg Config) error {
	path = Expand(path)
	unlock, err := privatefile.Lock(configLockPath(path))
	if err != nil {
		return err
	}
	defer unlock()
	return saveUnlocked(path, cfg)
}

// Update keeps the source read and replacement write under one lock so stale
// callers cannot overwrite a newer managed config publication.
func Update(path string, mutate func(*Config) error) error {
	path = Expand(path)
	unlock, err := privatefile.Lock(configLockPath(path))
	if err != nil {
		return err
	}
	defer unlock()
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	cfg, err := LoadPartialBytes(body)
	if err != nil {
		return err
	}
	if err := mutate(&cfg); err != nil {
		return err
	}
	return saveUnlocked(path, cfg)
}

// SaveIfUnchanged serializes exact-source validation with publication so one
// managed writer cannot overwrite authority another managed writer replaced.
func SaveIfUnchanged(path string, cfg Config, expected []byte, expectedExists bool) error {
	path = Expand(path)
	unlock, err := privatefile.Lock(configLockPath(path))
	if err != nil {
		return err
	}
	defer unlock()
	current, err := os.ReadFile(path)
	currentExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if currentExists != expectedExists || currentExists && !bytes.Equal(current, expected) {
		return ErrSourceChanged
	}
	return saveUnlocked(path, cfg)
}

func configLockPath(path string) string {
	return path + configLockSuffix
}

func saveUnlocked(path string, cfg Config) error {
	applyDefaults(&cfg)
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return privatefile.AtomicWrite(path, body, privatefile.WriteOptions{TempPattern: ".config-*.tmp", Sync: true})
}

func validateNamespaceIdentity(namespace, project string) error {
	if namespace != "" && project != "" && namespace != project {
		return fmt.Errorf("namespace %q conflicts with legacy project %q", namespace, project)
	}
	return nil
}
