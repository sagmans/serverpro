package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareConfigForCreateRejectsMalformedSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serverpro.yaml")
	if err := os.WriteFile(path, []byte("namespace: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, unlock, err := (&app{configPath: path, nonInteractive: true}).prepareConfigForCreate(context.Background(), "demo", "web")
	if err == nil {
		if unlock != nil {
			unlock()
		}
		t.Fatal("malformed source accepted")
	}
}

func TestPrepareConfigForCreateNonInteractiveRequiresProject(t *testing.T) {
	a := &app{nonInteractive: true}
	_, _, unlock, err := a.prepareConfigForCreate(context.Background(), "", "")
	if unlock != nil {
		unlock()
	}
	if err == nil || !strings.Contains(err.Error(), "managed config missing") {
		t.Fatalf("expected managed config error, got %v", err)
	}
}
