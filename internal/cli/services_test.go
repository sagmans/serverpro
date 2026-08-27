package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
)

func TestPreflightRejectsUnsupportedManagedImageBeforeNetworkChecks(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	cfg.Compute.Image = "debian-12"
	creds := credentials.Set{ServerProvider: "provider-token", Tailscale: "tailscale-token"}
	provider := cliFakeProvider{catalog: func(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
		return compute.Catalog{Images: []compute.Image{{Name: "debian-12", Architecture: "x86", OSFlavor: "debian", OSVersion: "12"}}}, nil
	}}
	a := &app{provider: "hetzner", providers: testRegistryWithProvider(t, provider)}
	if err := a.preflight(context.Background(), cfg, creds); err == nil || !strings.Contains(err.Error(), "unsupported managed image") {
		t.Fatalf("unsupported managed image error = %v", err)
	}
}

func TestPreflightRejectsMissingManagedImageBeforeNetworkChecks(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	cfg.Compute.Image = "missing-image"
	creds := credentials.Set{ServerProvider: "provider-token", Tailscale: "tailscale-token"}
	a := &app{provider: "hetzner", providers: testProviderRegistry(t)}
	if err := a.preflight(context.Background(), cfg, creds); err == nil || !strings.Contains(err.Error(), "not present in provider catalog") {
		t.Fatalf("missing managed image error = %v", err)
	}
}

func TestPreflightRejectsComputeAuthorityBeforeNetworkChecks(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	creds := credentials.Set{ServerProvider: "provider-token", Tailscale: "tailscale-token"}
	t.Run("unknown provider", func(t *testing.T) {
		a := &app{provider: "unknown", providers: compute.NewRegistry()}
		if err := a.preflight(context.Background(), cfg, creds); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unknown provider error = %v", err)
		}
	})
	t.Run("provider diagnostics", func(t *testing.T) {
		provider := cliFakeProvider{doctor: func(_ context.Context, account compute.Account) compute.Diagnostics {
			if account.Token != creds.ServerProvider || account.Scope != "demo/web" {
				t.Fatalf("account = %+v", account)
			}
			return compute.Diagnostics{{Status: compute.Fail, Message: "credential rejected"}}
		}}
		a := &app{provider: "hetzner", providers: testRegistryWithProvider(t, provider)}
		if err := a.preflight(context.Background(), cfg, creds); err == nil || !strings.Contains(err.Error(), "credential rejected") {
			t.Fatalf("provider diagnostic error = %v", err)
		}
	})
}
