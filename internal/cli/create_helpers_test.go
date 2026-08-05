package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/doctor"
	"github.com/assagman/serverpro/internal/state"
)

func createTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func createTestConfig(t *testing.T) string {
	t.Helper()
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	cfg := config.ExampleServer("demo", "web")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Project: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func passingCreateDoctorReport(context.Context, config.Config, state.State, credentials.Set, string, string) doctor.Report {
	return doctor.Report{Results: []doctor.Result{{Name: "smoke", Scope: "test", Status: doctor.Pass, Evidence: "ok"}}}
}

func writeDoctorFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	stPath := filepath.Join(dir, "state.json")
	cfg := config.ExampleServer("demo", "web")
	cfg.Cloudflare.AccountID = "acc"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Project: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	st := state.State{Project: "demo", Server: "web", Tailscale: state.TailscaleState{Name: "demo-web"}}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	return cfgPath, stPath
}
