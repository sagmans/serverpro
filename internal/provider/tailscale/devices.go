package tailscale

import (
	"context"
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

func (c Client) WaitDevice(ctx context.Context, hostname string, tags []string) (Device, error) {
	want := strings.TrimSuffix(hostname, ".")
	var lastErr error
	for {
		devices, err := c.Devices(ctx)
		if err != nil {
			lastErr = err
		} else {
			for _, d := range devices {
				if mesh.DeviceMatches(d, want, tags) && (d.Online || d.ConnectedToControl) {
					return d, nil
				}
			}
		}
		if err := poll.Wait(ctx, c.wait, devicePollInterval); err != nil {
			if lastErr != nil {
				return Device{}, fmt.Errorf("tailscale device %s not online: %w; last API error: %v", hostname, err, lastErr)
			}
			return Device{}, fmt.Errorf("tailscale device %s not online: %w", hostname, err)
		}
	}
}
