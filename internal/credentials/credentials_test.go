package credentials

import (
	"testing"

	"github.com/assagman/serverpro/internal/config"
)

func credentialsTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func testConfig(project, path string) config.Config {
	cfg := config.ExampleServer(project, "server")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Credentials.JSONPath = path
	return cfg
}
