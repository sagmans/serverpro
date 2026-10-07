package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/mesh"
)

func TestPreflightRejectsUnsupportedManagedImageBeforeNetworkChecks(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	cfg.Compute.Image = "debian-12"
	creds := credentials.Set{ServerProvider: "provider-token", Tailscale: "tailscale-token"}
	provider := cliFakeProvider{catalog: func(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
		return compute.Catalog{Images: []compute.Image{{Name: "debian-12", Architecture: "x86", OSFlavor: "debian", OSVersion: "12"}}}, nil
	}}
	a := &app{provider: "hetzner", providers: testRegistryWithProvider(t, provider)}
	if err := a.preflight(context.Background(), cfg, creds, false); err == nil || !strings.Contains(err.Error(), "unsupported managed image") {
		t.Fatalf("unsupported managed image error = %v", err)
	}
}

func TestPreflightRejectsMissingManagedImageBeforeNetworkChecks(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	cfg.Compute.Image = "missing-image"
	creds := credentials.Set{ServerProvider: "provider-token", Tailscale: "tailscale-token"}
	a := &app{provider: "hetzner", providers: testProviderRegistry(t)}
	if err := a.preflight(context.Background(), cfg, creds, false); err == nil || !strings.Contains(err.Error(), "not present in provider catalog") {
		t.Fatalf("missing managed image error = %v", err)
	}
}

func TestPreflightRejectsComputeAuthorityBeforeNetworkChecks(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	creds := credentials.Set{ServerProvider: "provider-token", Tailscale: "tailscale-token"}
	t.Run("unknown provider", func(t *testing.T) {
		a := &app{provider: "unknown", providers: compute.NewRegistry()}
		if err := a.preflight(context.Background(), cfg, creds, false); err == nil || !strings.Contains(err.Error(), "not found") {
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
		if err := a.preflight(context.Background(), cfg, creds, false); err == nil || !strings.Contains(err.Error(), "credential rejected") {
			t.Fatalf("provider diagnostic error = %v", err)
		}
	})
}

// preflightPolicyStub stands in for the tailnet so preflight tests can tell
// whether the compute checks let the run reach the network checks.
type preflightPolicyStub struct{ err error }

func (s preflightPolicyStub) Policy(context.Context) (mesh.Policy, error) {
	return mesh.Policy{}, s.err
}

// The size offer check guards new orders only: a resumed create must still
// reach the tailnet checks when the provider has withdrawn the recorded size.
func TestPreflightSizeOfferCheckSkipsResumedCompute(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	cfg.Compute.Size = "withdrawn-size"
	creds := credentials.Set{ServerProvider: "provider-token", Tailscale: "tailscale-token"}
	reachedTailnet := errors.New("reached tailnet checks")
	provider := cliFakeProvider{catalog: func(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
		return compute.Catalog{
			Sizes:  []compute.Size{{Name: "offered-size"}},
			Images: []compute.Image{{Name: cfg.Compute.Image, Architecture: "x86", OSFlavor: "ubuntu", OSVersion: "24.04"}},
		}, nil
	}}
	a := &app{provider: "hetzner", providers: testRegistryWithProvider(t, provider)}
	a.services.preflightTailscaleClient = func(string, string) preflightTailscaleClient {
		return preflightPolicyStub{err: reachedTailnet}
	}
	if err := a.preflight(context.Background(), cfg, creds, false); err == nil || !strings.Contains(err.Error(), "is not available in location") {
		t.Fatalf("new order size error = %v", err)
	}
	if err := a.preflight(context.Background(), cfg, creds, true); !errors.Is(err, reachedTailnet) {
		t.Fatalf("resumed compute error = %v, want tailnet checks reached", err)
	}
}
