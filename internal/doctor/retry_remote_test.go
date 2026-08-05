package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

type failingRemote struct{}

func (failingRemote) Run(context.Context, string, string, string) (string, error) {
	return "", context.DeadlineExceeded
}

func TestRetryRemoteReplacesOnlyRemoteScopes(t *testing.T) {
	cfg := config.Example("prod")
	existing := Report{
		Inventory: []InventoryItem{
			{Scope: "provider", Name: "compute server", Value: "id=2"},
			{Scope: "remote", Name: "host", Value: "old host"},
		},
		Results: []Result{
			{Scope: "provider", Name: "tailscale node", Status: Pass, Evidence: "api_reported_online=false ssh=ok"},
			{Scope: "remote", Name: SudoPasswordCheckName, Status: Fail, Code: SudoPasswordAuthFailureCode, Evidence: "old auth failure"},
			{Scope: "provider", Name: "cloudflare connector", Status: Pass, Evidence: "healthy"},
		},
	}
	runner := &fakeRemote{}
	got := RetryRemote(context.Background(), cfg, state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}, existing, runner, Options{})
	if len(got.Inventory) != 2 || got.Inventory[0] != existing.Inventory[0] || strings.Contains(got.Inventory[1].Value, "old host") {
		t.Fatalf("retry inventory = %+v", got.Inventory)
	}
	if len(got.Results) < 3 || got.Results[0].Name != "tailscale node" || got.Results[len(got.Results)-1].Name != "cloudflare connector" {
		t.Fatalf("retry result ordering = %+v", got.Results)
	}
	if hasResult(got, SudoPasswordCheckName, Fail, "old auth failure") || !hasResult(got, SudoPasswordCheckName, Pass, "ok") {
		t.Fatalf("remote results not replaced: %+v", got.Results)
	}
	if !hasResult(got, "tailscale node", Pass, "ssh=ok") {
		t.Fatalf("tailscale SSH annotation not recomputed: %+v", got.Results)
	}
}

func TestRetryRemoteClearsStaleSSHAnnotationWhenRetryFails(t *testing.T) {
	cfg := config.Example("prod")
	existing := Report{Results: []Result{{Scope: "provider", Name: "tailscale node", Status: Pass, Evidence: "api_reported_online=false ssh=ok"}}}
	got := RetryRemote(context.Background(), cfg, state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}, existing, failingRemote{}, Options{})
	if strings.Contains(got.Results[0].Evidence, "ssh=ok") {
		t.Fatalf("stale SSH annotation retained: %+v", got.Results[0])
	}
}
