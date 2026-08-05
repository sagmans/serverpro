package importsync

import (
	"context"
	"fmt"
	"strings"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/state"
)

// MatchTailscaleDevice finds the mesh node for an imported compute hostname.
func MatchTailscaleDevice(ctx context.Context, client tailscale.Client, candidate Candidate, cfg config.Config) (state.TailscaleState, error) {
	devices, err := client.Devices(ctx)
	if err != nil {
		return state.TailscaleState{}, fmt.Errorf("tailscale device list failed: %w", err)
	}
	wantHost := strings.TrimSuffix(candidate.Name, ".")
	if wantHost == "" {
		wantHost = strings.TrimSuffix(cfg.Compute.Name, ".")
	}
	tags := cfg.Access.Tailscale.Tags
	var matches []tailscale.Device
	for _, device := range devices {
		if deviceMatches(device, wantHost, tags) {
			matches = append(matches, device)
		}
	}
	if len(matches) == 0 {
		// Fall back to hostname-only when tags drifted but name still matches uniquely.
		for _, device := range devices {
			if deviceMatches(device, wantHost, nil) {
				matches = append(matches, device)
			}
		}
	}
	if len(matches) == 0 {
		return state.TailscaleState{}, fmt.Errorf("tailscale device %q not found", wantHost)
	}
	if len(matches) > 1 {
		return state.TailscaleState{}, fmt.Errorf("tailscale device %q is ambiguous", wantHost)
	}
	device := matches[0]
	return state.TailscaleState{
		NodeID: device.NodeID,
		Name:   device.Name,
		IPs:    append([]string(nil), device.Addresses...),
		Tags:   append([]string(nil), device.Tags...),
	}, nil
}

func deviceMatches(device tailscale.Device, hostname string, tags []string) bool {
	if hostname == "" {
		return false
	}
	if device.Hostname != hostname && device.Name != hostname && !strings.HasPrefix(device.Name, hostname+".") {
		return false
	}
	for _, tag := range tags {
		if !containsString(device.Tags, tag) {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
