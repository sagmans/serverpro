package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sagmans/serverpro/internal/privatefile"
)

func LoadRegistry(path string) (Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewRegistry(), nil
		}
		return Registry{}, err
	}
	var file struct {
		Registry
		Projects map[string]RegistryNamespace `json:"projects"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return Registry{}, err
	}
	r := file.Registry
	r.Namespaces, err = mergeRegistryNamespaces(r.Namespaces, file.Projects)
	if err != nil {
		return Registry{}, err
	}
	r.normalize()
	return r, nil
}

func mergeRegistryNamespaces(canonical, legacy map[string]RegistryNamespace) (map[string]RegistryNamespace, error) {
	merged := make(map[string]RegistryNamespace, len(canonical)+len(legacy))
	copyNamespace := func(namespace string, group RegistryNamespace, rejectConflicts bool) error {
		target := merged[namespace]
		if target.Servers == nil {
			target.Servers = map[string]RegistryEntry{}
		}
		for server, entry := range group.Servers {
			entry.Namespace = namespace
			if entry.Server == "" {
				entry.Server = server
			}
			if existing, ok := target.Servers[server]; ok && rejectConflicts {
				existing.Namespace = namespace
				if existing.Server == "" {
					existing.Server = server
				}
				if !sameRegistryAuthority(existing, entry) {
					return fmt.Errorf("conflicting registry entries for %s/%s", namespace, server)
				}
				continue
			}
			target.Servers[server] = entry
		}
		merged[namespace] = target
		return nil
	}
	for namespace, group := range canonical {
		if err := copyNamespace(namespace, group, false); err != nil {
			return nil, err
		}
	}
	for namespace, group := range legacy {
		// Canonical and legacy sections can coexist during interrupted migrations;
		// union distinct entries but never choose between conflicting authorities.
		if err := copyNamespace(namespace, group, true); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func sameRegistryAuthority(a, b RegistryEntry) bool {
	return a.Namespace == b.Namespace &&
		a.Server == b.Server &&
		a.StatePath == b.StatePath &&
		a.ConfigPath == b.ConfigPath &&
		a.CredentialsPath == b.CredentialsPath &&
		a.ResourceNames == b.ResourceNames
}

func SaveRegistry(path string, r Registry) error {
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
	if len(r.Namespaces) == 0 {
		if err := privatefile.RemoveDurably(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return saveRegistryUnlocked(path, r)
}

func saveRegistryUnlocked(path string, r Registry) error {
	r.normalize()
	r.UpdatedAt = time.Now().UTC()
	return privatefile.AtomicWriteJSON(path, r, privatefile.WriteOptions{TempPattern: ".registry-*.tmp", Sync: true})
}
