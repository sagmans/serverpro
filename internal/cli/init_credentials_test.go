package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestEnsureCredentialsRejectsEmptyPromptWithoutSaving(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "default")
	cfg.Cloudflare.AccountID = "acc"
	a := &app{stdin: strings.NewReader("\n\n\n"), stdout: io.Discard}
	_, saved, err := a.ensureCredentials(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing credentials") {
		t.Fatalf("expected missing credentials, saved=%v err=%v", saved, err)
	}
	if saved {
		t.Fatal("empty credentials reported as saved")
	}
	if _, err := os.Stat(config.Expand(cfg.Credentials.JSONPath)); !os.IsNotExist(err) {
		t.Fatalf("empty credentials saved, stat err=%v", err)
	}
}
