package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestSaveRejectsEmptyCredentialPath(t *testing.T) {
	err := Save(testConfig("prod", ""), Set{ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"})
	if err == nil || !strings.Contains(err.Error(), "credentials.json_path") {
		t.Fatalf("expected credentials path error, got %v", err)
	}
}

func TestSaveUsesConfigNamespace(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	cfg := testConfig("prod", path)
	if err := Save(cfg, Set{Namespace: "other", ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	creds, err := loadJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Namespace != "prod" {
		t.Fatalf("namespace = %q", creds.Namespace)
	}
}

func TestSaveWritesPrivateJSON(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	if err := Save(testConfig("prod", path), Set{ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	creds, err := Load(testConfig("prod", path))
	if err != nil {
		t.Fatal(err)
	}
	if creds.Namespace != "prod" || creds.Server != "server" || creds.ServerProvider != "h" {
		t.Fatalf("saved namespace = %q server = %q server_provider=%q", creds.Namespace, creds.Server, creds.ServerProvider)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"namespace": "prod"`) || !strings.Contains(string(body), `"server": "server"`) || !strings.Contains(string(body), `"server_provider_token": "h"`) || strings.Contains(string(body), `"project"`) {
		t.Fatalf("credentials JSON did not use namespace schema:\n%s", string(body))
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", di.Mode().Perm())
	}
}

func TestSaveTightensExistingCredentialFileMode(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(testConfig("prod", path), Set{ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
