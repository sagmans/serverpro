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
