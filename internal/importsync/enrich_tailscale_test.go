package importsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
)

func TestMatchTailscaleDeviceTracksTailnetIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tailnet/example.ts.net/users":
			if r.URL.Query().Get("type") != "member" {
				t.Fatalf("missing member filter: %s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"users":[{"type":"member","tailnetId":"tailnet-1"}]}`))
		case "/tailnet/example.ts.net/devices":
			_, _ = w.Write([]byte(`{"devices":[{"nodeId":"node-1","name":"demo-web.example.ts.net","hostname":"demo-web","addresses":["100.64.0.1"],"tags":["tag:serverpro-demo"]}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	cfg := config.ExampleServer("demo", "web")
	cfg.Access.Tailscale.Tailnet = "example.ts.net"
	client := tailscale.NewWithHTTP("token", cfg.Access.Tailscale.Tailnet, server.URL, server.Client())

	got, err := MatchTailscaleDevice(context.Background(), client, Candidate{Name: "demo-web"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tailnet != "example.ts.net" || got.TailnetID != "tailnet-1" || got.NodeID != "node-1" {
		t.Fatalf("imported Tailscale identity incomplete: %+v", got)
	}
}

func TestMatchTailscaleDeviceStopsWhenTailnetIdentityIsUnavailable(t *testing.T) {
	deviceListed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/users") {
			_, _ = w.Write([]byte(`{"users":[]}`))
			return
		}
		deviceListed = true
	}))
	defer server.Close()
	cfg := config.ExampleServer("demo", "web")
	client := tailscale.NewWithHTTP("token", cfg.Access.Tailscale.Tailnet, server.URL, server.Client())

	_, err := MatchTailscaleDevice(context.Background(), client, Candidate{Name: "demo-web"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "tailnet identity") {
		t.Fatalf("expected tailnet identity error, got %v", err)
	}
	if deviceListed {
		t.Fatal("device lookup ran without canonical tailnet identity")
	}
}
