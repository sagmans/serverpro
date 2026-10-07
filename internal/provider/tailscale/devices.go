package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/poll"
)

const devicePollInterval = 5 * time.Second

func (c Client) Devices(ctx context.Context) ([]Device, error) {
	var out struct {
		Devices []Device `json:"devices"`
	}
	err := c.api.Do(ctx, http.MethodGet, "/tailnet/"+url.PathEscape(c.tailnet)+"/devices", nil, &out)
	return out.Devices, err
}

func (c Client) DeleteDevice(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return c.api.Do(ctx, http.MethodDelete, "/device/"+url.PathEscape(id), nil, nil)
}

// WaitDevice polls until exactly one device satisfies q and is reachable.
// Only "not enrolled yet" and "enrolled but offline" are worth waiting for;
// ambiguity or a changed recorded device returns at once so callers stop
// before any privileged traffic reaches a node.
func (c Client) WaitDevice(ctx context.Context, q mesh.DeviceQuery) (Device, error) {
	q.Hostname = strings.TrimSuffix(q.Hostname, ".")
	var lastErr error
	for {
		devices, err := c.Devices(ctx)
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil
			d, err := mesh.SelectDevice(devices, q)
			switch {
			case err == nil && (d.Online || d.ConnectedToControl):
				return d, nil
			case err != nil && !errors.Is(err, mesh.ErrDeviceNotFound):
				return Device{}, fmt.Errorf("tailscale device %s: %w", q.Hostname, err)
			}
		}
		if err := poll.Wait(ctx, c.wait, devicePollInterval); err != nil {
			if lastErr != nil {
				return Device{}, fmt.Errorf("tailscale device %s not online: %w; last API error: %v", q.Hostname, err, lastErr)
			}
			return Device{}, fmt.Errorf("tailscale device %s not online: %w", q.Hostname, err)
		}
	}
}
