package cli

import (
	"path/filepath"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func TestUpsertRegistryEntryStoresExpandedConfigMetadata(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	cfg := config.ExampleServer("prod", "web")
	cfg.Credentials.JSONPath = "~/.config/serverpro/namespaces/prod/credentials.json"
	cfg.Compute.Name = "prod-web"
	cfg.Compute.Labels = map[string]string{"serverpro.namespace": "prod", "serverpro.server": "web"}
	cfg.Cloudflare.Tunnel.Name = "prod-web"

	a := &app{configPath: cfgPath}
	statePath := config.ServerStatePath("prod", "web")
	if err := a.upsertRegistryEntry(cfg, statePath); err != nil {
		t.Fatal(err)
	}

	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Find("prod", "web")
	if !ok {
		t.Fatal("missing registry entry")
	}
	if entry.ConfigPath != cfgPath || entry.StatePath != statePath || entry.CredentialsPath != cfg.Credentials.JSONPath {
		t.Fatalf("bad registry paths: %+v", entry)
	}
	if entry.ResourceNames.ComputeServer != "prod-web" || entry.ResourceNames.CloudflareTunnel != "prod-web" || entry.Labels["serverpro.server"] != "web" {
		t.Fatalf("bad registry metadata: %+v", entry)
	}
}
