package cli

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func TestCreateSetupWritesRegistryEntry(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	a := &app{configPath: cfgPath, stdin: strings.NewReader(createPromptInput()), stdout: io.Discard}
	cfg, stPath, err := a.prepareConfig("demo", "web")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.ensureCredentials(cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.upsertRegistryEntry(cfg, stPath); err != nil {
		t.Fatal(err)
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Find("demo", "web")
	if !ok {
		t.Fatal("missing registry entry")
	}
	if entry.ConfigPath != cfgPath || entry.StatePath != config.ServerStatePath("demo", "web") || entry.ResourceNames.ComputeServer != "demo-web" || entry.Labels["serverpro.server"] != "web" {
		t.Fatalf("bad registry entry: %+v", entry)
	}
}
