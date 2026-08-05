package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func createDryRunConfigPath(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return dir, filepath.Join(dir, "serverpro.yaml")
}

func TestCreateDryRunShowsPlanWithoutCredentials(t *testing.T) {
	dir, cfgPath := createDryRunConfigPath(t)
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Credentials.JSONPath = filepath.Join(dir, "namespaces", "prod", "servers", config.DefaultServer(), "credentials.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", dryRun: true, nonInteractive: true, stdout: &out}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{config.DefaultServer()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "compute server") || strings.Contains(out.String(), "missing credentials") {
		t.Fatalf("bad dry-run output: %s", out.String())
	}
}

func TestCreateDryRunDoesNotRewritePartialConfig(t *testing.T) {
	_, cfgPath := createDryRunConfigPath(t)
	body := strings.Join([]string{
		"compute:",
		"  name: prod-01",
		"cloudflare:",
		"  account_id: acc",
		"  tunnel:",
		"    name: prod-01",
	}, "\n") + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{configPath: cfgPath, project: "prod", provider: "hetzner", dryRun: true, nonInteractive: true, stdout: io.Discard}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{config.DefaultServer()})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "compute.location") || !strings.Contains(err.Error(), "compute.size") || !strings.Contains(err.Error(), "compute.image") {
		t.Fatalf("expected missing explicit compute selections, got %v", err)
	}
	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != body {
		t.Fatalf("dry-run rewrote config:\n%s", string(saved))
	}
}

func TestCreateDryRunDoesNotWriteStateOrRegistry(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", dryRun: true, nonInteractive: true, stdout: &out}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{config.DefaultServer()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.ServerStatePath("prod", config.DefaultServer())); !os.IsNotExist(err) {
		t.Fatalf("dry-run created state, stat err=%v", err)
	}
	if _, err := os.Stat(config.RegistryPath()); !os.IsNotExist(err) {
		t.Fatalf("dry-run created registry, stat err=%v", err)
	}
}
