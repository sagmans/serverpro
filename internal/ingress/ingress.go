package ingress

import (
	"context"
	"fmt"
)

type Type string

const CloudflareTunnel Type = "cloudflare-tunnel"

const StatusPending = "pending"

type Route struct {
	Type     Type   `json:"type"`
	Hostname string `json:"hostname"`
	Target   string `json:"target,omitempty"`
	Status   string `json:"status,omitempty"`
}

type Adapter interface {
	Add(context.Context, Route) (Route, error)
	Remove(context.Context, Route) error
}

type CloudflareTunnelAdapter struct{}

func (CloudflareTunnelAdapter) Add(_ context.Context, route Route) (Route, error) {
	if route.Hostname == "" {
		return Route{}, fmt.Errorf("hostname required")
	}
	route.Type = CloudflareTunnel
	// Cloudflare mutations are not wired here; pending prevents false success claims.
	route.Status = StatusPending
	return route, nil
}

func (CloudflareTunnelAdapter) Remove(_ context.Context, route Route) error {
	if route.Hostname == "" {
		return fmt.Errorf("hostname required")
	}
	return nil
}
