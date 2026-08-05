package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/state"
)

func TestEnsureCloudflareTunnelRetryAdoptsTunnelCreatedBeforeCheckpoint(t *testing.T) {
	cfg := cloudflareProvisionConfig()
	client := &fakeCloudflare{tunnels: []cloudflare.Tunnel{{ID: "existing", Name: cfg.Cloudflare.Tunnel.Name}, {ID: "other", Name: "other"}}}
	st := state.State{}
	var saved state.State

	err := ensureCloudflareTunnel(context.Background(), &st, "state.json", cfg, client, func(_ string, got state.State) error {
		saved = got
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.createCalls != 0 || st.Cloudflare.TunnelID != "existing" || st.Cloudflare.Provenance != state.CloudflareTunnelAdopted || saved.Cloudflare.Provenance != state.CloudflareTunnelAdopted {
		t.Fatalf("existing tunnel not adopted: creates=%d state=%+v saved=%+v", client.createCalls, st.Cloudflare, saved.Cloudflare)
	}
}

func TestEnsureCloudflareTunnelRejectsAmbiguousExistingNames(t *testing.T) {
	cfg := cloudflareProvisionConfig()
	client := &fakeCloudflare{tunnels: []cloudflare.Tunnel{{ID: "one", Name: cfg.Cloudflare.Tunnel.Name}, {ID: "two", Name: cfg.Cloudflare.Tunnel.Name}}}
	st := state.State{}

	err := ensureCloudflareTunnel(context.Background(), &st, "state.json", cfg, client, func(string, state.State) error { return nil })
	if err == nil || !strings.Contains(err.Error(), `tunnel "prod-web" is ambiguous`) || client.createCalls != 0 {
		t.Fatalf("expected provider-neutral ambiguity without create, creates=%d err=%v", client.createCalls, err)
	}
}

func TestEnsureCloudflareTunnelRollsBackFreshTunnelOnCheckpointFailure(t *testing.T) {
	cfg := cloudflareProvisionConfig()
	client := &fakeCloudflare{}
	st := state.State{}
	saveErr := errors.New("checkpoint failed")

	err := ensureCloudflareTunnel(context.Background(), &st, "state.json", cfg, client, func(string, state.State) error { return saveErr })
	if !errors.Is(err, saveErr) || len(client.deletedTunnelIDs) != 1 || client.deletedTunnelIDs[0] != "tun1" {
		t.Fatalf("fresh tunnel not rolled back: deleted=%v err=%v", client.deletedTunnelIDs, err)
	}
	if st.Cloudflare.TunnelID != "tun1" || st.Cloudflare.Provenance != state.CloudflareTunnelCreated {
		t.Fatalf("rollback discarded recovery evidence: %+v", st.Cloudflare)
	}
}

func TestEnsureCloudflareTunnelRollbackSurvivesCanceledProvisionContext(t *testing.T) {
	cfg := cloudflareProvisionConfig()
	client := &fakeCloudflare{}
	st := state.State{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ensureCloudflareTunnel(ctx, &st, "state.json", cfg, client, func(string, state.State) error {
		return errors.New("checkpoint failed")
	})
	if err == nil || client.deleteCtxErr != nil {
		t.Fatalf("rollback reused canceled provision context: delete context error=%v err=%v", client.deleteCtxErr, err)
	}
}

func TestEnsureCloudflareTunnelDoesNotDeleteAdoptedTunnelOnCheckpointFailure(t *testing.T) {
	cfg := cloudflareProvisionConfig()
	client := &fakeCloudflare{tunnels: []cloudflare.Tunnel{{ID: "existing", Name: cfg.Cloudflare.Tunnel.Name}}}
	st := state.State{}
	saveErr := errors.New("checkpoint failed")

	err := ensureCloudflareTunnel(context.Background(), &st, "state.json", cfg, client, func(string, state.State) error { return saveErr })
	if !errors.Is(err, saveErr) || client.createCalls != 0 || len(client.deletedTunnelIDs) != 0 || st.Cloudflare.Provenance != state.CloudflareTunnelAdopted {
		t.Fatalf("adopted tunnel was treated as fresh: creates=%d deleted=%v state=%+v err=%v", client.createCalls, client.deletedTunnelIDs, st.Cloudflare, err)
	}
}

func TestEnsureCloudflareTunnelPreservesCheckpointAndRollbackFailures(t *testing.T) {
	cfg := cloudflareProvisionConfig()
	rollbackErr := errors.New("rollback failed")
	client := &fakeCloudflare{deleteErr: rollbackErr}
	st := state.State{}
	saveErr := errors.New("checkpoint failed")

	err := ensureCloudflareTunnel(context.Background(), &st, "state.json", cfg, client, func(string, state.State) error { return saveErr })
	if !errors.Is(err, saveErr) || !errors.Is(err, rollbackErr) || st.Cloudflare.TunnelID != "tun1" {
		t.Fatalf("failure evidence lost: state=%+v err=%v", st.Cloudflare, err)
	}
}

func cloudflareProvisionConfig() config.Config {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	return cfg
}
