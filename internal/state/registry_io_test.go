package state

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	registryProcessHelperEnv    = "SERVERPRO_REGISTRY_PROCESS_HELPER"
	registryProcessPathEnv      = "SERVERPRO_REGISTRY_PROCESS_PATH"
	registryProcessServerEnv    = "SERVERPRO_REGISTRY_PROCESS_SERVER"
	registryProcessWorkerCount  = 8
	registryProcessOverlapDelay = 20 * time.Millisecond
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

func TestLoadRegistryMigratesLegacyProjectSchema(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	legacy := `{"schema_version":1,"projects":{"prod":{"servers":{"web":{"project":"prod","server":"web","state_path":"/state/prod/web.json"}}}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Find("prod", "web")
	if !ok || entry.Namespace != "prod" {
		t.Fatalf("entry = %+v, found=%t", entry, ok)
	}
	if err := SaveRegistry(path, reg); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"projects"`) || strings.Contains(string(body), `"project"`) || !strings.Contains(string(body), `"namespaces"`) {
		t.Fatalf("registry migration = %s", body)
	}
}

func TestLoadRegistryUnionsCanonicalAndLegacyNamespaces(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	mixed := `{"schema_version":1,"namespaces":{"prod":{"servers":{"web":{"namespace":"prod","server":"web","state_path":"/state/prod/web.json"}}}},"projects":{"prod":{"servers":{"api":{"project":"prod","server":"api","state_path":"/state/prod/api.json"}}},"legacy":{"servers":{"worker":{"project":"legacy","server":"worker","state_path":"/state/legacy/worker.json"}}}}}`
	if err := os.WriteFile(path, []byte(mixed), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range [][2]string{{"prod", "web"}, {"prod", "api"}, {"legacy", "worker"}} {
		if _, ok := reg.Find(target[0], target[1]); !ok {
			t.Fatalf("mixed registry lost %s/%s: %+v", target[0], target[1], reg)
		}
	}
}

func TestLoadRegistryRejectsConflictingCanonicalAndLegacyEntry(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	mixed := `{"schema_version":1,"namespaces":{"prod":{"servers":{"web":{"namespace":"prod","server":"web","state_path":"/state/prod/web.json"}}}},"projects":{"prod":{"servers":{"web":{"project":"prod","server":"web","state_path":"/other/prod/web.json"}}}}}`
	if err := os.WriteFile(path, []byte(mixed), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRegistry(path)
	if err == nil || !strings.Contains(err.Error(), "conflicting registry entries for prod/web") {
		t.Fatalf("error = %v", err)
	}
}

func TestSaveLoadRegistryFile(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "web", StatePath: "/state/prod/web.json", ConfigPath: "/cfg/prod.yaml", CredentialsPath: "/creds/prod.json"})

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
	if entry.StatePath != "/state/prod/web.json" || entry.Namespace != "prod" || entry.Server != "web" {
		t.Fatalf("bad entry: %+v", entry)
	}
}

func TestUpdateRegistryCoordinatesConcurrentProcesses(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	commands := make([]*exec.Cmd, registryProcessWorkerCount)
	outputs := make([]bytes.Buffer, registryProcessWorkerCount)
	for worker := range commands {
		server := fmt.Sprintf("worker-%d", worker)
		cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateRegistryProcessHelper$")
		cmd.Env = append(os.Environ(),
			registryProcessHelperEnv+"=1",
			registryProcessPathEnv+"="+path,
			registryProcessServerEnv+"="+server,
		)
		cmd.Stdout = &outputs[worker]
		cmd.Stderr = &outputs[worker]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands[worker] = cmd
	}
	for worker, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("worker %d: %v\n%s", worker, err, outputs[worker].String())
		}
	}
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := reg.List("prod")
	if len(entries) != registryProcessWorkerCount {
		t.Fatalf("concurrent processes preserved %d/%d entries: %+v", len(entries), registryProcessWorkerCount, entries)
	}
}

func TestUpdateRegistryProcessHelper(t *testing.T) {
	if os.Getenv(registryProcessHelperEnv) != "1" {
		return
	}
	path := os.Getenv(registryProcessPathEnv)
	server := os.Getenv(registryProcessServerEnv)
	if path == "" || server == "" {
		t.Fatal("registry process helper environment incomplete")
	}
	if err := UpdateRegistry(path, func(reg *Registry) error {
		// Delay inside the transaction so an absent process lock deterministically
		// exposes multiple writers operating on the same snapshot.
		time.Sleep(registryProcessOverlapDelay)
		reg.Upsert(RegistryEntry{Namespace: "prod", Server: server, StatePath: filepath.Join("/state/prod", server+".json")})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRegistryPreservesEmptyNamespaceFile(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "web", StatePath: "/state/prod/web.json"})
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
		reg.RemoveNamespace("prod")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected removed registry file, got %v", err)
	}
}

func TestUpdateRegistryDurablyRemovesEmptyRegistry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	reg := NewRegistry()
	reg.UpsertNamespace("prod")
	if err := SaveRegistry(path, reg); err != nil {
		t.Fatal(err)
	}
	err := UpdateRegistry(path, func(reg *Registry) error {
		reg.RemoveNamespace("prod")
		return os.Chmod(dir, 0o300)
	})
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if err == nil {
		t.Fatal("registry removal did not sync the unreadable parent directory")
	}
}

func TestUpdateRegistryPreservesFileOnCallbackError(t *testing.T) {
	path := stateTestPath(t, "registry.json")
	reg := NewRegistry()
	reg.Upsert(RegistryEntry{Namespace: "prod", Server: "web", StatePath: "/state/prod/web.json"})
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
