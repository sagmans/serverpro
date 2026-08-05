package state

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/assagman/serverpro/internal/privatefile"
)

func LoadRegistry(path string) (Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewRegistry(), nil
		}
		return Registry{}, err
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return Registry{}, err
	}
	if err := validateSchemaVersion("registry", r.SchemaVersion, RegistrySchemaVersion); err != nil {
		return Registry{}, err
	}
	r.normalize()
	return r, nil
}

func SaveRegistry(path string, r Registry) error {
	if err := validateSchemaVersion("registry", r.SchemaVersion, RegistrySchemaVersion); err != nil {
		return err
	}
	unlock, err := lockRegistry(path)
	if err != nil {
		return err
	}
	defer unlock()
	return saveRegistryUnlocked(path, r)
}

func UpdateRegistry(path string, fn func(*Registry) error) error {
	unlock, err := lockRegistry(path)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := LoadRegistry(path)
	if err != nil {
		return err
	}
	if err := fn(&r); err != nil {
		return err
	}
	r.normalize()
	if len(r.Projects) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return saveRegistryUnlocked(path, r)
}

func saveRegistryUnlocked(path string, r Registry) error {
	if err := validateSchemaVersion("registry", r.SchemaVersion, RegistrySchemaVersion); err != nil {
		return err
	}
	r.normalize()
	r.UpdatedAt = time.Now().UTC()
	return privatefile.AtomicWriteJSON(path, r, privatefile.WriteOptions{TempPattern: ".registry-*.tmp"})
}
