package state

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLoadMissingReturnsEmptyRegistry(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.List("")) != 0 {
		t.Fatalf("expected empty registry: %+v", reg)
	}
}

func TestLoadRegistryPreservesUnversionedSchema(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	if err := os.WriteFile(path, []byte(`{"namespaces":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if reg.SchemaVersion != RegistrySchemaVersion {
		t.Fatalf("schema version = %d", reg.SchemaVersion)
	}
}

func TestLoadRegistryRejectsUnsupportedSchema(t *testing.T) {
	for _, version := range []int{-1, RegistrySchemaVersion + 1} {
		path := stateTestPath(t, "registry.json")
		body := []byte(fmt.Sprintf(`{"schema_version":%d,"namespaces":{}}`, version))
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRegistry(path); err == nil || !strings.Contains(err.Error(), "unsupported registry schema version") {
			t.Fatalf("LoadRegistry schema %d error = %v", version, err)
		}
	}
}

func TestSaveRegistryRejectsUnsupportedSchema(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.SchemaVersion++
	if err := SaveRegistry(path, reg); err == nil || !strings.Contains(err.Error(), "unsupported registry schema version") {
		t.Fatalf("SaveRegistry error = %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported registry write created file: %v", statErr)
	}
}

func TestSaveLoadRegistryFile(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Project: "prod", Server: "web", StatePath: "/state/prod/web.json", ConfigPath: "/cfg/prod.yaml", CredentialsPath: "/creds/prod.json"})

	if err := SaveRegistry(path, reg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"namespaces"`) || !strings.Contains(text, `"namespace": "prod"`) || strings.Contains(text, `"projects"`) || strings.Contains(text, `"project":`) {
		t.Fatalf("registry did not use namespace schema:\n%s", text)
	}

	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := loaded.Find("prod", "web")
	if !ok {
		t.Fatalf("missing prod/web: %+v", loaded)
	}
	if entry.StatePath != "/state/prod/web.json" || entry.Project != "prod" || entry.Server != "web" {
		t.Fatalf("bad entry: %+v", entry)
	}
}

func TestUpdateRegistryPreservesEmptyNamespaceFile(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Project: "prod", Server: "web", StatePath: "/state/prod/web.json"})
	if err := SaveRegistry(path, reg); err != nil {
		t.Fatal(err)
	}

	if err := UpdateRegistry(path, func(reg *Registry) error {
		reg.Remove("prod", "web")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.ListNamespaces(); len(got) != 1 || got[0] != "prod" {
		t.Fatalf("namespace should remain after last server removal: %+v", got)
	}
}

func TestUpdateRegistryRemovesFileWhenNoNamespaceRemains(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.UpsertNamespace("prod")
	if err := SaveRegistry(path, reg); err != nil {
		t.Fatal(err)
	}

	if err := UpdateRegistry(path, func(reg *Registry) error {
		reg.RemoveProject("prod")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected removed registry file, got %v", err)
	}
}

func TestUpdateRegistryPreservesFileOnCallbackError(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Project: "prod", Server: "web", StatePath: "/state/prod/web.json"})
	if err := SaveRegistry(path, reg); err != nil {
		t.Fatal(err)
	}

	want := errors.New("stop")
	err := UpdateRegistry(path, func(reg *Registry) error {
		reg.Remove("prod", "web")
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Find("prod", "web"); !ok {
		t.Fatal("callback error removed registry entry")
	}
}
