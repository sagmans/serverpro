package importsync

import (
	"context"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/state"
)

// MatchCloudflareTunnel finds a tunnel whose name matches the compute resource name.
type tunnelLister interface {
	ListTunnels(context.Context) ([]ingress.Tunnel, error)
}

func MatchCloudflareTunnel(ctx context.Context, client tunnelLister, candidate Candidate, cfg config.Config) (state.CloudflareState, error) {
	tunnels, err := client.ListTunnels(ctx)
	if err != nil {
		return state.CloudflareState{}, fmt.Errorf("cloudflare tunnel list failed: %w", err)
	}
	want := candidate.Name
	if want == "" {
		want = cfg.Compute.Name
	}
	if want == "" {
		want = cfg.Cloudflare.Tunnel.Name
	}
	match, found, err := ingress.MatchTunnelByName(tunnels, want)
	if err != nil {
		return state.CloudflareState{}, err
	}
	if !found {
		return state.CloudflareState{}, nil
	}
	return state.CloudflareState{TunnelID: match.ID, Name: match.Name, Provenance: state.CloudflareTunnelImported}, nil
}
