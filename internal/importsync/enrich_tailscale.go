package importsync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/state"
)

// MatchTailscaleDevice finds the mesh node for an imported compute hostname.
type meshDeviceLister interface {
	Devices(context.Context) ([]mesh.Device, error)
}

func MatchTailscaleDevice(ctx context.Context, client meshDeviceLister, candidate Candidate, cfg config.Config) (state.TailscaleState, error) {
	devices, err := client.Devices(ctx)
	if err != nil {
		return state.TailscaleState{}, fmt.Errorf("tailscale device list failed: %w", err)
	}
	wantHost := strings.TrimSuffix(candidate.Name, ".")
	if wantHost == "" {
		wantHost = strings.TrimSuffix(cfg.Compute.Name, ".")
	}
	tags := cfg.Access.Tailscale.Tags
	device, err := mesh.SelectDevice(devices, mesh.DeviceQuery{Hostname: wantHost, Tags: tags})
	if errors.Is(err, mesh.ErrDeviceNotFound) {
		// Fall back to hostname-only when tags drifted but name still matches uniquely.
		device, err = mesh.SelectDevice(devices, mesh.DeviceQuery{Hostname: wantHost})
	}
	if err != nil {
		return state.TailscaleState{}, fmt.Errorf("tailscale device lookup failed: %w", err)
	}
	return state.TailscaleState{
		Tailnet: cfg.Access.Tailscale.Tailnet,
		NodeID:  device.NodeID,
		Name:    device.Name,
		IPs:     append([]string(nil), device.Addresses...),
		Tags:    append([]string(nil), device.Tags...),
	}, nil
}
