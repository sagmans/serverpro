package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
)

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
