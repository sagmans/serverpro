package cli

import (
	"path/filepath"
	"testing"

	"github.com/assagman/serverpro/internal/config"
)

func TestConfigPathHelpersUseManagedTargetPaths(t *testing.T) {
	createTestHome(t)

	a := &app{}
	if got := a.initialConfigPath("", ""); got != "" {
		t.Fatalf("initialConfigPath without project = %q", got)
	}
	if got, want := a.initialConfigPath("prod", "web"), config.ServerConfigPath("prod", "web"); got != want {
		t.Fatalf("initialConfigPath = %q, want %q", got, want)
	}
	if got, want := a.resolvedConfigPath(config.ExampleServer("prod", "api")), config.ServerConfigPath("prod", "api"); got != want {
		t.Fatalf("resolvedConfigPath = %q, want %q", got, want)
	}
}

func TestConfigPathHelpersHonorExplicitConfigPath(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "serverpro.yaml")
	a := &app{configPath: explicit}

	if got := a.initialConfigPath("prod", "web"); got != explicit {
		t.Fatalf("initialConfigPath = %q, want explicit path", got)
	}
	if got := a.resolvedConfigPath(config.ExampleServer("prod", "web")); got != explicit {
		t.Fatalf("resolvedConfigPath = %q, want explicit path", got)
	}
}
