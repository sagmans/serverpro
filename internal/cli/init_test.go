package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
)

func createPromptInput() string {
	return strings.Join([]string{"", "", "", "", "", "", "", "", "", "h", "ts"}, "\n") + "\n"
}

func TestCreateSetupCollectsConfigAndAuth(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	var out bytes.Buffer
	a := &app{configPath: cfgPath, stdin: strings.NewReader(createPromptInput()), stdout: &out}
	cfg, stPath, err := a.prepareConfig("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.ensureCredentials(cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.upsertRegistryEntry(cfg, stPath); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "demo" || cfg.Cloudflare.AccountID != "" || cfg.Network.Ingress != "none" {
		t.Fatalf("bad config: %+v", cfg)
	}
	wantCredPath := filepath.Join(dir, ".config", "serverpro", "namespaces", "demo", "servers", config.DefaultServer(), "credentials.json")
	if cfg.Credentials.JSONPath != wantCredPath {
		t.Fatalf("credentials path = %q", cfg.Credentials.JSONPath)
	}
	creds, err := credentials.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.ServerProvider != "h" || creds.Tailscale != "ts" || creds.Cloudflare != "" {
		t.Fatalf("bad credentials: %+v", creds)
	}
	if !strings.Contains(out.String(), "auth saved") {
		t.Fatalf("bad output: %s", out.String())
	}
}
