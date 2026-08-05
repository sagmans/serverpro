package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func TestNamespaceCreateRegistersTopLevelNamespace(t *testing.T) {
	dir := createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"namespace", "create", "demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row struct {
		Status     string `json:"status"`
		Namespace  string `json:"namespace"`
		ConfigPath string `json:"config_path"`
		StatePath  string `json:"state_path"`
	}
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("namespace create output is not JSON: %s", out.String())
	}
	if row.Status != "created" || row.Namespace != "demoapp" {
		t.Fatalf("bad namespace row: %+v", row)
	}
	wantConfig := filepath.Join(dir, ".config", "serverpro", "namespaces", "demoapp")
	wantState := filepath.Join(dir, ".local", "state", "serverpro", "namespaces", "demoapp")
	if row.ConfigPath != wantConfig || row.StatePath != wantState {
		t.Fatalf("bad namespace paths: %+v", row)
	}
	for _, path := range []string{wantConfig, wantState} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("namespace dir missing %s: %v", path, err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("bad namespace dir %s mode=%o is_dir=%t", path, info.Mode().Perm(), info.IsDir())
		}
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.ListNamespaces(); len(got) != 1 || got[0] != "demoapp" {
		t.Fatalf("namespaces = %+v", got)
	}
}

func TestNamespaceListShowsRegisteredNamespaces(t *testing.T) {
	createTestHome(t)
	reg := state.NewRegistry()
	reg.UpsertNamespace("sampleapp")
	reg.Upsert(state.RegistryEntry{Project: "demoapp", Server: "web", StatePath: config.ServerStatePath("demoapp", "web")})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"namespace", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Namespace   string `json:"namespace"`
		ServerCount int    `json:"server_count"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("namespace list output is not JSON: %s", out.String())
	}
	if len(rows) != 2 || rows[0].Namespace != "demoapp" || rows[0].ServerCount != 1 || rows[1].Namespace != "sampleapp" || rows[1].ServerCount != 0 {
		t.Fatalf("bad namespace rows: %+v", rows)
	}
}

func TestNamespaceCreateDryRunDoesNotWriteLocalState(t *testing.T) {
	dir := createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--dry-run", "namespace", "create", "demoapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row struct {
		Status    string `json:"status"`
		DryRun    bool   `json:"dry_run"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("namespace dry-run output is not JSON: %s", out.String())
	}
	if row.Status != "planned" || !row.DryRun || row.Namespace != "demoapp" {
		t.Fatalf("bad dry-run row: %+v", row)
	}
	for _, path := range []string{config.NamespaceConfigDir("demoapp"), config.NamespaceStateDir("demoapp"), config.RegistryPath()} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("dry-run wrote %s under %s", path, dir)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
}
