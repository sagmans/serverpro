package ingress

import (
	"context"
	"testing"
)

func TestCloudflareTunnelAdapterReportsLocalPendingState(t *testing.T) {
	route, err := CloudflareTunnelAdapter{}.Add(context.Background(), Route{Hostname: "app.example.com", Target: "tailnet-host"})
	if err != nil {
		t.Fatal(err)
	}
	if route.Type != CloudflareTunnel || route.Hostname != "app.example.com" || route.Target != "tailnet-host" || route.Status != "pending" {
		t.Fatalf("bad route: %+v", route)
	}
}

func TestCloudflareTunnelAdapterAddRequiresHostname(t *testing.T) {
	if _, err := (CloudflareTunnelAdapter{}).Add(context.Background(), Route{}); err == nil {
		t.Fatal("expected hostname requirement on add")
	}
}

func TestCloudflareTunnelAdapterRemoveAcceptsHostname(t *testing.T) {
	if err := (CloudflareTunnelAdapter{}).Remove(context.Background(), Route{Hostname: "app.example.com"}); err != nil {
		t.Fatalf("remove of known hostname should succeed: %v", err)
	}
}

func TestCloudflareTunnelAdapterRemoveRequiresHostname(t *testing.T) {
	if err := (CloudflareTunnelAdapter{}).Remove(context.Background(), Route{}); err == nil {
		t.Fatal("expected hostname requirement on remove")
	}
}
