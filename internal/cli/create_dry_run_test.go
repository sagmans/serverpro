package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
)

func TestCreateDryRunFreshTargetShowsPlanWithoutLocalWrites(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	a := &app{project: "demo", provider: "hetzner", dryRun: true, nonInteractive: true, stdout: &out}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web", "--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "compute server") || !strings.Contains(out.String(), "size=cx23 image=ubuntu-24.04 location=fsn1") {
		t.Fatalf("bad dry-run output: %s", out.String())
	}
	if fileExists(config.RegistryPath()) {
		t.Fatal("dry-run wrote registry")
	}
	if fileExists(config.ServerConfigPath("demo", "web")) {
		t.Fatal("dry-run wrote config")
	}
}

func TestCreateDryRunFreshTargetRequiresExplicitCatalogSelections(t *testing.T) {
	createTestHome(t)
	a := &app{project: "demo", provider: "digitalocean", dryRun: true, nonInteractive: true, stdout: io.Discard}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "compute.location") || !strings.Contains(err.Error(), "compute.size") || !strings.Contains(err.Error(), "compute.image") {
		t.Fatalf("expected missing explicit compute selections, got %v", err)
	}
	if fileExists(config.RegistryPath()) {
		t.Fatal("dry-run wrote registry")
	}
	if fileExists(config.ServerConfigPath("demo", "web")) {
		t.Fatal("dry-run wrote config")
	}
}

func TestCreateDryRunRejectsUnknownProvider(t *testing.T) {
	createTestHome(t)
	a := &app{project: "demo", provider: "digital ocean", dryRun: true, nonInteractive: true, stdout: io.Discard}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web", "--location", "nyc3", "--size", "s-1vcpu-1gb", "--image", "ubuntu-24-04-x64"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `provider "digital ocean" not found`) {
		t.Fatalf("expected precise provider validation, got %v", err)
	}
}

func TestCreateDryRunFreshTargetRejectsInvalidNamespaceWithoutLocalWrites(t *testing.T) {
	createTestHome(t)
	a := &app{project: "Demo", provider: "hetzner", dryRun: true, nonInteractive: true, stdout: io.Discard}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web", "--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("expected invalid namespace error, got %v", err)
	}
	if fileExists(config.RegistryPath()) {
		t.Fatal("dry-run wrote registry")
	}
	if fileExists(config.ServerConfigPath("Demo", "web")) {
		t.Fatal("dry-run wrote config")
	}
}

func TestServerCreateFlagsCoverPromptedDetails(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"server", "create", "web", "-n", "mynamespace", "-p", "hetzner", "--dry-run", "--non-interactive", "--compute-name", "mynamespace-web", "--location", "nbg1", "--size", "cpx31", "--image", "ubuntu-24.04", "--admin-user", "ops", "--tailscale-tailnet", "example.ts.net", "--tailscale-tags", "tag:serverpro-mynamespace", "--ingress", "none", "--egress-mode", "open"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "size=cpx31 image=ubuntu-24.04 location=nbg1") || !strings.Contains(out.String(), "open best-effort policy") {
		t.Fatalf("create flags did not drive preview config:\n%s", out.String())
	}
}

func TestCreateDryRunRejectsImagesIncompatibleWithBootstrap(t *testing.T) {
	for _, image := range []string{"debian-12", "ubuntu-22.04", "windows-2025", "custom-image"} {
		t.Run(image, func(t *testing.T) {
			createTestHome(t)
			a := &app{project: "demo", provider: "hetzner", dryRun: true, nonInteractive: true, stdout: io.Discard}
			cmd := a.serverCreateCmd()
			cmd.SetArgs([]string{"web", "--location", "fsn1", "--size", "cx23", "--image", image})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "Ubuntu 24.04") {
				t.Fatalf("image %q should fail compatibility validation, got %v", image, err)
			}
		})
	}
}

func TestCreatePreviewRejectsMissingCatalogSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*config.Config)
		want string
	}{
		{name: "location", edit: func(cfg *config.Config) { cfg.Compute.Location = "" }, want: "compute.location"},
		{name: "size", edit: func(cfg *config.Config) { cfg.Compute.Size = "" }, want: "compute.size"},
		{name: "image", edit: func(cfg *config.Config) { cfg.Compute.Image = "" }, want: "compute.image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.ExampleServer("demo", "web")
			cfg.Cloudflare.AccountID = "acc"
			tc.edit(&cfg)
			a := &app{provider: "hetzner"}
			err := a.validateCreatePreviewTarget(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestCreateDryRunMissingExplicitConfigReportsPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")
	a := &app{configPath: path, provider: "hetzner", dryRun: true, nonInteractive: true, stdout: io.Discard}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("expected config path error, got %v", err)
	}
}
