package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
)

func TestCreateDryRunMissingConfigDoesNotWriteLocalFiles(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	a := &app{configPath: cfgPath, provider: "hetzner", dryRun: true, nonInteractive: true, stdin: strings.NewReader(""), stdout: io.Discard}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("expected missing config error, got %v", err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote config, stat err=%v", err)
	}
	if _, err := os.Stat(config.RegistryPath()); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote registry, stat err=%v", err)
	}
}
