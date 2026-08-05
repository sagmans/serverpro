package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unscopedCredentialsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "credentials.json")
}

func TestLoadRejectsUnscopedCredentialsPath(t *testing.T) {
	cfg := testConfig("prod", unscopedCredentialsPath(t))
	_, err := LoadPartial(cfg)
	if err == nil || !strings.Contains(err.Error(), "credentials.json_path") {
		t.Fatalf("expected credentials path error, got %v", err)
	}
}

func TestLoadRejectsCredentialPathOutsideConfigRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	outside := filepath.Join(dir, "outside", "namespaces", "prod", "servers", "server", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte(`{"namespace":"prod","server":"server","server_provider_token":"h","tailscale_token":"ts","cloudflare_token":"cf"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(testConfig("prod", outside))
	if err == nil || !strings.Contains(err.Error(), "outside serverpro config root") {
		t.Fatalf("expected config root refusal, got %v", err)
	}
}

func TestSaveRejectsUnscopedCredentialsPath(t *testing.T) {
	cfg := testConfig("prod", unscopedCredentialsPath(t))
	err := Save(cfg, Set{ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"})
	if err == nil || !strings.Contains(err.Error(), "credentials.json_path") {
		t.Fatalf("expected credentials path error, got %v", err)
	}
}

func TestSaveRejectsCredentialPathOutsideConfigRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	outside := filepath.Join(dir, "outside", "namespaces", "prod", "servers", "server", "credentials.json")
	err := Save(testConfig("prod", outside), Set{ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"})
	if err == nil || !strings.Contains(err.Error(), "outside serverpro config root") {
		t.Fatalf("expected config root refusal, got %v", err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside credentials written, stat err=%v", err)
	}
}

func TestSaveRejectsCredentialSymlinkAncestor(t *testing.T) {
	dir := credentialsTestHome(t)
	configRoot := filepath.Join(dir, ".config", "serverpro")
	outside := filepath.Join(dir, "outside")
	linked := filepath.Join(configRoot, "namespaces", "prod", "servers", "server")
	path := filepath.Join(linked, "credentials.json")
	if err := os.MkdirAll(filepath.Dir(linked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	err := Save(testConfig("prod", path), Set{ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"})
	if err == nil || !strings.Contains(err.Error(), "symlink credentials path") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("outside credentials written through symlink, stat err=%v", err)
	}
}

func TestSaveRejectsCredentialSymlinkFile(t *testing.T) {
	dir := credentialsTestHome(t)
	path := filepath.Join(dir, ".config", "serverpro", "namespaces", "prod", "servers", "server", "credentials.json")
	outside := filepath.Join(dir, "outside.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	err := Save(testConfig("prod", path), Set{ServerProvider: "h", Tailscale: "ts", Cloudflare: "cf"})
	if err == nil || !strings.Contains(err.Error(), "symlink credentials path") {
		t.Fatalf("expected symlink file refusal, got %v", err)
	}
	b, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{}" {
		t.Fatalf("outside symlink target overwritten: %s", string(b))
	}
}
