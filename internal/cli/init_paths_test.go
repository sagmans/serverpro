package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestCreateSetupWithoutConfigWritesManagedConfig(t *testing.T) {
	createTestHome(t)
	work := t.TempDir()
	t.Chdir(work)
	a := &app{stdin: strings.NewReader(createPromptInput()), stdout: io.Discard}
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
	cfgPath := config.ServerConfigPath("demo", "web")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("managed config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "serverpro.yaml")); !os.IsNotExist(err) {
		t.Fatalf("cwd config should not be written, stat err=%v", err)
	}
}
