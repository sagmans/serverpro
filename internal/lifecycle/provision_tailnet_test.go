package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/state"
)

func TestEnsureTailscaleTailnetIdentityCheckpointsFreshState(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Project: cfg.Project, Server: cfg.Server}
	statePath := provisionStatePath(t)
	client := &fakeTailscale{tailnetID: "tailnet-1"}

	if err := ensureTailscaleTailnetIdentity(context.Background(), &st, statePath, credentials.Set{Tailscale: "ts-api"}, cfg, client); err != nil {
		t.Fatal(err)
	}
	if st.Tailscale.Tailnet != "-" || st.Tailscale.TailnetID != "tailnet-1" {
		t.Fatalf("tailnet identity missing: %+v", st.Tailscale)
	}
	persisted, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Tailscale.Tailnet != "-" || persisted.Tailscale.TailnetID != "tailnet-1" {
		t.Fatalf("tailnet identity was not checkpointed: %+v", persisted.Tailscale)
	}
}

func TestEnsureTailscaleTailnetIdentityFailsClosedForLegacyResources(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Project: cfg.Project, Server: cfg.Server, Tailscale: state.TailscaleState{AuthKeyID: "key-1"}}
	client := &fakeTailscale{}

	err := ensureTailscaleTailnetIdentity(context.Background(), &st, provisionStatePath(t), credentials.Set{Tailscale: "ts-api"}, cfg, client)
	if err == nil || !strings.Contains(err.Error(), "tailnet identity missing") {
		t.Fatalf("expected legacy identity error, got %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("legacy guard contacted mutable tailnet: %v", client.calls)
	}
}

func TestEnsureTailscaleTailnetIdentityRejectsCredentialDrift(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{
		Project: cfg.Project,
		Server:  cfg.Server,
		Tailscale: state.TailscaleState{
			Tailnet:   "-",
			TailnetID: "tailnet-1",
			AuthKeyID: "key-1",
		},
	}
	client := &fakeTailscale{tailnetID: "tailnet-2"}

	err := ensureTailscaleTailnetIdentity(context.Background(), &st, provisionStatePath(t), credentials.Set{Tailscale: "ts-api"}, cfg, client)
	if err == nil || !strings.Contains(err.Error(), "tailnet identity mismatch") {
		t.Fatalf("expected drift error, got %v", err)
	}
	if strings.Join(client.calls, ",") != "tailnet-id" {
		t.Fatalf("unexpected calls before drift rejection: %v", client.calls)
	}
}
