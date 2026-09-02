package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/privatefile"
)

func TestLoadAppliesDefaultsExceptCatalogSelections(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\nadmin:\n  username: deploy\ncompute:\n  name: prod-01\n  location: fsn1\n  size: cx23\n  image: ubuntu-24.04\ncloudflare:\n  account_id: acc\n  tunnel:\n    name: prod-01\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Namespace != "prod" || cfg.Admin.Username != "deploy" || cfg.Compute.Image != "ubuntu-24.04" || cfg.Network.Egress.Mode != "restricted" || !cfg.Hardening.UFW {
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

func TestLoadPartialBytesUsesProvidedSnapshot(t *testing.T) {
	cfg, err := LoadPartialBytes([]byte("namespace: approved\ncompute:\n  location: snapshot-location\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Namespace != "approved" || cfg.Compute.Location != "snapshot-location" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadPartialPreservesExplicitLockdownFalse(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\nnetwork:\n  egress:\n    phase_lockdown_after_bootstrap: false\n")
	cfg, err := LoadPartial(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Network.Egress.PhaseLockdownAfterBootstrap {
		t.Fatal("explicit false lockdown phase was overwritten by defaults")
	}
}

func TestLoadPartialMigratesLegacyProjectField(t *testing.T) {
	path := writeConfigFixture(t, "project: prod\n")
	cfg, err := LoadPartial(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Namespace != "prod" {
		t.Fatalf("namespace = %q", cfg.Namespace)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "namespace: prod") || strings.Contains(string(body), "project:") {
		t.Fatalf("legacy config was not migrated:\n%s", body)
	}
}

func TestLoadPartialRejectsDivergentNamespaceAndProject(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\nproject: other\n")
	_, err := LoadPartial(path)
	if err == nil || !strings.Contains(err.Error(), `namespace "prod" conflicts with legacy project "other"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestSaveIfUnchangedRejectsSourceDrift(t *testing.T) {
	for _, test := range []struct {
		name           string
		initial        string
		replacement    string
		expectedExists bool
	}{
		{name: "edited", initial: "namespace: approved\n", replacement: "namespace: replacement\n", expectedExists: true},
		{name: "removed", initial: "namespace: approved\n", expectedExists: true},
		{name: "appeared", replacement: "namespace: replacement\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "serverpro.yaml")
			if test.initial != "" {
				if err := os.WriteFile(path, []byte(test.initial), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if test.replacement == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(test.replacement), 0o600); err != nil {
				t.Fatal(err)
			}
			err := SaveIfUnchanged(path, ExampleServer("approved", "web"), []byte(test.initial), test.expectedExists)
			if !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("source drift error = %v", err)
			}
		})
	}
}

func TestSaveIfUnchangedValidatesAfterAcquiringConfigLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serverpro.yaml")
	approved := []byte("namespace: approved\n")
	if err := os.WriteFile(path, approved, 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := privatefile.Lock(configLockPath(path))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- SaveIfUnchanged(path, ExampleServer("approved", "web"), approved, true) }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("conditional save bypassed config lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	replacement := []byte("namespace: replacement\n")
	if err := os.WriteFile(path, replacement, 0o600); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSourceChanged) {
			t.Fatalf("source drift error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("conditional save did not resume after config lock release")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(replacement) {
		t.Fatalf("replacement source overwritten:\n%s", body)
	}
}

func TestSaveWaitsForLocalArtifactCleanup(t *testing.T) {
	configTestHome(t)
	path := ServerConfigPath("approved", "web")
	unlock, err := privatefile.LockExclusiveContext(context.Background(), LocalArtifactGuardPath())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Save(path, ExampleServer("approved", "web")) }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("save bypassed artifact cleanup guard: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("save did not resume after artifact cleanup")
	}
}

func TestSaveCoordinatesConfigWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serverpro.yaml")
	unlock, err := privatefile.Lock(configLockPath(path))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Save(path, ExampleServer("approved", "web")) }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("save bypassed config lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("save did not resume after config lock release")
	}
}

func TestUpdateReadsAndWritesUnderConfigLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serverpro.yaml")
	if err := os.WriteFile(path, []byte("namespace: approved\ncompute:\n  location: initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := privatefile.Lock(configLockPath(path))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Update(path, func(cfg *Config) error {
			cfg.Admin.Username = "ops"
			return nil
		})
	}()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("config update read before acquiring config lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	replacement := []byte("namespace: approved\ncompute:\n  location: replacement\n")
	if err := os.WriteFile(path, replacement, 0o600); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("config update did not resume after lock release")
	}
	cfg, err := LoadPartial(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Compute.Location != "replacement" || cfg.Admin.Username != "ops" {
		t.Fatalf("config update lost concurrent source: %+v", cfg)
	}
}

func TestUpdatePreservesSourceWhenMutationFails(t *testing.T) {
	path := writeConfigFixture(t, "namespace: approved\n")
	expected := errors.New("mutation rejected")
	if err := Update(path, func(*Config) error { return expected }); !errors.Is(err, expected) {
		t.Fatalf("update error = %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "namespace: approved\n" {
		t.Fatalf("failed update changed source:\n%s", body)
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

func TestLoadPartialRejectsUnsupportedLegacyFields(t *testing.T) {
	for _, tt := range []struct {
		name  string
		body  string
		field string
	}{
		{name: "credentials mode", body: "credentials:\n  mode: json\n", field: "mode"},
		{name: "egress allow list", body: "network:\n  egress:\n    allow: [dns]\n", field: "allow"},
		{name: "emergency SSH", body: "access:\n  emergency_ssh:\n    enabled: false\n", field: "emergency_ssh"},
		{name: "tunnel smoke route", body: "cloudflare:\n  tunnel:\n    smoke_route:\n      enabled: false\n", field: "smoke_route"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFixture(t, tt.body)
			_, err := LoadPartial(path)
			if err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("expected unsupported %s error, got %v", tt.field, err)
			}
		})
	}
}

func TestLoadPartialAcceptsSupportedSchema(t *testing.T) {
	path := writeConfigFixture(t, "namespace: prod\nserver: web\ncredentials:\n  json_path: /tmp/credentials.json\ncompute:\n  name: prod-web\n  location: fsn1\n  size: cx23\n  image: ubuntu-24.04\n  labels:\n    custom: yes\nadmin:\n  username: deploy\n  store_console_password: false\nnetwork:\n  ingress: none\n  egress:\n    mode: restricted\n    phase_lockdown_after_bootstrap: true\naccess:\n  public_ssh: false\n  tailscale:\n    enabled: true\n    ssh: true\n    tailnet: '-'\n    tags: [tag:serverpro-prod]\n    root_policy: check-or-disabled\ncloudflare:\n  account_id: ''\n  tunnel:\n    enabled: false\n    name: prod-web\n    create_connector_only: false\nhardening:\n  profile: strict\n  unattended_upgrades: true\n  apparmor: true\n  ufw: true\n  journald_persistent: true\n")
	cfg, err := LoadPartial(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Namespace != "prod" || cfg.Server != "web" || cfg.Credentials.JSONPath != "/tmp/credentials.json" {
		t.Fatalf("config = %+v", cfg)
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
