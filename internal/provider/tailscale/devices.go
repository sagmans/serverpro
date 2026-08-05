package tailscale

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

type DeviceWait struct {
	Hostname    string
	Tags        []string
	ExcludedIDs []string
	DeviceID    string
}

func (c Client) MatchingDeviceIDs(ctx context.Context, hostname string, tags []string) ([]string, error) {
	devices, err := c.Devices(ctx)
	if err != nil {
		return nil, err
	}
	want := strings.TrimSuffix(hostname, ".")
	var ids []string
	seen := map[string]bool{}
	for _, device := range devices {
		if !matches(device, want, tags) {
			continue
		}
		for _, id := range deviceIdentifiers(device) {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	return ids, nil
}

func (c Client) WaitDevice(ctx context.Context, request DeviceWait) (Device, error) {
	want := strings.TrimSuffix(request.Hostname, ".")
	excluded := stringSet(request.ExcludedIDs)
	var lastErr error
	for {
		devices, err := c.Devices(ctx)
		if err != nil {
			lastErr = err
		} else {
			candidates := matchingDeviceCandidates(devices, want, request, excluded)
			if len(candidates) > 1 {
				return Device{}, fmt.Errorf("tailscale device %s is ambiguous: %d newly enrolled devices match", request.Hostname, len(candidates))
			}
			if len(candidates) == 1 {
				candidate := candidates[0]
				if len(deviceIdentifiers(candidate)) == 0 {
					return Device{}, fmt.Errorf("tailscale device %s has no stable device ID", request.Hostname)
				}
				if candidate.Online || candidate.ConnectedToControl {
					return candidate, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return Device{}, fmt.Errorf("tailscale device %s not online: %w; last API error: %v", request.Hostname, ctx.Err(), lastErr)
			}
			return Device{}, fmt.Errorf("tailscale device %s not online: %w", request.Hostname, ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}

func matchingDeviceCandidates(devices []Device, hostname string, request DeviceWait, excluded map[string]bool) []Device {
	var candidates []Device
	for _, device := range devices {
		if !matches(device, hostname, request.Tags) {
			continue
		}
		ids := deviceIdentifiers(device)
		if request.DeviceID != "" {
			if contains(ids, request.DeviceID) {
				candidates = append(candidates, device)
			}
			continue
		}
		if !containsAny(ids, excluded) {
			candidates = append(candidates, device)
		}
	}
	return candidates
}

func deviceIdentifiers(device Device) []string {
	ids := make([]string, 0, 2)
	if device.ID != "" {
		ids = append(ids, device.ID)
	}
	if device.NodeID != "" && device.NodeID != device.ID {
		ids = append(ids, device.NodeID)
	}
	return ids
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func containsAny(values []string, set map[string]bool) bool {
	for _, value := range values {
		if set[value] {
			return true
		}
	}
	return false
}

func matches(d Device, hostname string, tags []string) bool {
	if d.Hostname != hostname && d.Name != hostname && !strings.HasPrefix(d.Name, hostname+".") {
		return false
	}
	for _, tag := range tags {
		if !contains(d.Tags, tag) {
			return false
		}
	}
	return true
}
