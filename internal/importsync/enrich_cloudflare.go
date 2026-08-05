package importsync

import (
	"context"
	"fmt"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/provider/cloudflare"
	"github.com/assagman/serverpro/internal/state"
)

// MatchCloudflareTunnel finds a tunnel whose name matches the compute resource name.
func MatchCloudflareTunnel(ctx context.Context, client cloudflare.Client, candidate Candidate, cfg config.Config) (state.CloudflareState, error) {
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
	var matches []cloudflare.Tunnel
	for _, tunnel := range tunnels {
		if tunnel.Name == want {
			matches = append(matches, tunnel)
		}
	}
	if len(matches) == 0 {
		return state.CloudflareState{}, nil
	}
	if len(matches) > 1 {
		return state.CloudflareState{}, fmt.Errorf("cloudflare tunnel %q is ambiguous", want)
	}
	return state.CloudflareState{TunnelID: matches[0].ID, Name: matches[0].Name}, nil
}
