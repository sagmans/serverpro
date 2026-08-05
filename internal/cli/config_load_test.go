package cli

import (
	"path/filepath"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func TestLoadConfigOnlyAppliesTargetOverrides(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	a := &app{configPath: cfgPath, server: "api"}
	loaded, stPath, err := a.loadConfigWithState()
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Project != "prod" || loaded.Server != "api" {
		t.Fatalf("target = %s/%s", loaded.Project, loaded.Server)
	}
	if loaded.Compute.Name != "prod-api" || loaded.Cloudflare.Tunnel.Name != "prod-api" {
		t.Fatalf("resource names = %q/%q", loaded.Compute.Name, loaded.Cloudflare.Tunnel.Name)
	}
	if want := config.ServerStatePath("prod", "api"); stPath != want {
		t.Fatalf("state path = %q, want %q", stPath, want)
	}
}

func TestApplyConfigTargetOverridesRefreshesNamesForProjectOnlyOverride(t *testing.T) {
	cfg := config.ExampleServer("old", "web")
	applyConfigTargetOverrides(&cfg, "new", "")
	if cfg.Project != "new" || cfg.Server != "web" {
		t.Fatalf("target = %s/%s", cfg.Project, cfg.Server)
	}
	if cfg.Compute.Name != "new-web" || cfg.Cloudflare.Tunnel.Name != "new-web" {
		t.Fatalf("resource names = %q/%q", cfg.Compute.Name, cfg.Cloudflare.Tunnel.Name)
	}
}

func TestLoadConfigOnlyUsesRegistryTarget(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "configs", "prod.yaml")
	stPath := filepath.Join(dir, "states", "prod-web.json")
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{
		Project:       "prod",
		Server:        "web",
		StatePath:     stPath,
		ConfigPath:    cfgPath,
		ResourceNames: state.RegistryResourceNames{ComputeServer: "prod-blue", CloudflareTunnel: "prod-blue"},
	})
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}

	a := &app{project: "prod", server: "web"}
	loaded, gotStatePath, err := a.loadConfigWithState()
	if err != nil {
		t.Fatal(err)
	}

	if gotStatePath != stPath {
		t.Fatalf("state path = %q, want %q", gotStatePath, stPath)
	}
	if loaded.Project != "prod" || loaded.Server != "web" {
		t.Fatalf("target = %s/%s", loaded.Project, loaded.Server)
	}
	if loaded.Compute.Name != "prod-blue" || loaded.Cloudflare.Tunnel.Name != "prod-blue" {
		t.Fatalf("registry resource names not applied: %q/%q", loaded.Compute.Name, loaded.Cloudflare.Tunnel.Name)
	}
}
