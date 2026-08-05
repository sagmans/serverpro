package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppliesDefaultsExceptCatalogSelections(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\nadmin:\n  username: deploy\ncompute:\n  name: prod-01\n  location: fsn1\n  size: cx23\n  image: ubuntu-24.04\ncloudflare:\n  account_id: acc\n  tunnel:\n    name: prod-01\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "prod" || cfg.Namespace != "prod" || cfg.Admin.Username != "deploy" || cfg.Compute.Image != "ubuntu-24.04" || cfg.Network.Egress.Mode != "restricted" || !cfg.Hardening.UFW {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadDoesNotInventAdminUsername(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\ncompute:\n  name: prod-01\n  location: fsn1\n  size: cx23\n  image: ubuntu-24.04\ncloudflare:\n  account_id: acc\n  tunnel:\n    name: prod-01\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "admin.username") {
		t.Fatalf("expected missing admin.username, got %v", err)
	}
}

func TestLoadRequiresExplicitCatalogSelections(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\ncompute:\n  name: prod-01\ncloudflare:\n  account_id: acc\n  tunnel:\n    name: prod-01\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "compute.location") || !strings.Contains(err.Error(), "compute.size") || !strings.Contains(err.Error(), "compute.image") {
		t.Fatalf("expected missing explicit compute selections, got %v", err)
	}
}

func TestLoadPartialRejectsProjectField(t *testing.T) {
	path := writeConfigFixture(t, "project: prod\n")
	_, err := LoadPartial(path)
	if err == nil {
		t.Fatal("expected retired project field rejection")
	}
}

func TestSaveWritesNamespaceField(t *testing.T) {
	cfg := ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	path := filepath.Join(t.TempDir(), "serverpro.yaml")
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "namespace: prod") || strings.Contains(text, "project:") {
		t.Fatalf("config did not use namespace field:\n%s", text)
	}
}

func TestLoadPartialRejectsUnknownFields(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\nunknown: true\n")
	_, err := LoadPartial(path)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func writeConfigFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "serverpro.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
