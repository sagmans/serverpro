package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
)

type recordingTailscalePreflight struct {
	calls      []string
	tailnetErr error
}

func (f *recordingTailscalePreflight) TailnetID(context.Context) (string, error) {
	f.calls = append(f.calls, "users")
	return "tailnet-1", f.tailnetErr
}

func (f *recordingTailscalePreflight) Policy(context.Context) (tailscale.Policy, error) {
	f.calls = append(f.calls, "policy")
	return tailscale.Policy{}, nil
}

func TestValidateCreateImageReferenceRejectsUnsupportedImages(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider string
		image    string
		wantErr  bool
	}{
		{name: "hetzner ubuntu 24.04", provider: "hetzner", image: "ubuntu-24.04"},
		{name: "digitalocean ubuntu 24.04", provider: "digitalocean", image: "ubuntu-24-04-x64"},
		{name: "vultr ubuntu 24.04", provider: "vultr", image: "2284"},
		{name: "debian", provider: "hetzner", image: "debian-12", wantErr: true},
		{name: "old ubuntu", provider: "hetzner", image: "ubuntu-22.04", wantErr: true},
		{name: "windows", provider: "hetzner", image: "windows-2025", wantErr: true},
		{name: "unknown", provider: "hetzner", image: "custom-image", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := (&app{}).resolveProvider(tc.provider)
			if err != nil {
				t.Fatal(err)
			}
			err = validateCreateImageReference(provider, tc.image)
			if tc.wantErr && (err == nil || !strings.Contains(err.Error(), "Ubuntu 24.04")) {
				t.Fatalf("expected compatibility error, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPreflightRejectsIncompatibleImageBeforeServiceChecks(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	cfg.Compute.Size = "cpx22"
	cfg.Compute.Image = "ubuntu-22.04"
	a := &app{provider: "hetzner", providers: testProviderRegistry(t)}
	err := a.preflight(context.Background(), cfg, credentials.Set{ServerProvider: "token"})
	if err == nil || !strings.Contains(err.Error(), "Ubuntu 24.04") {
		t.Fatalf("expected image compatibility error, got %v", err)
	}
}

func TestPreflightTailscaleAccessRequiresMemberReadBeforePolicy(t *testing.T) {
	client := &recordingTailscalePreflight{tailnetErr: errors.New("users:read denied")}

	err := preflightTailscaleAccess(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), "users:read denied") {
		t.Fatalf("expected member-read error, got %v", err)
	}
	if strings.Join(client.calls, ",") != "users" {
		t.Fatalf("policy read ran before member access passed: %v", client.calls)
	}
}

func TestPreflightTailscaleAccessReadsMemberIdentityThenPolicy(t *testing.T) {
	client := &recordingTailscalePreflight{}

	if err := preflightTailscaleAccess(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if strings.Join(client.calls, ",") != "users,policy" {
		t.Fatalf("preflight calls = %v", client.calls)
	}
}

func TestValidateCreateCatalogSelectionRejectsArchitectureMismatch(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	catalog := compute.Catalog{
		Locations: []compute.Location{{Name: cfg.Compute.Location}},
		Sizes:     []compute.Size{{Name: cfg.Compute.Size, Architecture: "arm"}},
		Images:    []compute.Image{{Name: cfg.Compute.Image, Architecture: "x86", OSFlavor: "ubuntu", OSVersion: "24.04"}},
	}
	err := validateCreateCatalogSelection(catalog, cfg)
	if err == nil || !strings.Contains(err.Error(), "architecture") {
		t.Fatalf("expected architecture compatibility error, got %v", err)
	}
}

func TestValidateCreateCatalogSelectionRequiresAvailableSupportedTarget(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	catalog := compute.Catalog{
		Locations: []compute.Location{{Name: cfg.Compute.Location}},
		Sizes:     []compute.Size{{Name: cfg.Compute.Size}},
		Images:    []compute.Image{{Name: cfg.Compute.Image, OSFlavor: "ubuntu", OSVersion: "24.04"}},
	}
	if err := validateCreateCatalogSelection(catalog, cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*compute.Catalog)
		want string
	}{
		{name: "location", edit: func(c *compute.Catalog) { c.Locations = nil }, want: "location"},
		{name: "size", edit: func(c *compute.Catalog) { c.Sizes = nil }, want: "size"},
		{name: "image missing", edit: func(c *compute.Catalog) { c.Images = nil }, want: "image"},
		{name: "image incompatible", edit: func(c *compute.Catalog) { c.Images[0].OSVersion = "22.04" }, want: "Ubuntu 24.04"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := catalog
			candidate.Locations = append([]compute.Location(nil), catalog.Locations...)
			candidate.Sizes = append([]compute.Size(nil), catalog.Sizes...)
			candidate.Images = append([]compute.Image(nil), catalog.Images...)
			tc.edit(&candidate)
			err := validateCreateCatalogSelection(candidate, cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}
