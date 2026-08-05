package config

import (
	"bytes"
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

func LoadPartial(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(Expand(path))
	if err != nil {
		return cfg, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return cfg, err
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func Save(path string, cfg Config) error {
	applyDefaults(&cfg)
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return privatefile.AtomicWrite(Expand(path), b, privatefile.WriteOptions{TempPattern: ".config-*.tmp"})
}
