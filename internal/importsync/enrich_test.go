package importsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/sagmans/serverpro/internal/testhttp"
)

// WHY: import recovery reattaches mesh identity by hostname+tag. These matchers
// were the single largest 0% gap in the recovery path, so exercise the happy
// path, tag-drift fallback, ambiguity, and not-found branches directly.

func tailscaleDevicesServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/tailnet/") {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(handlerErr.Check)
	t.Cleanup(ts.Close)
	return ts
}

func tailscaleMatchConfig(tags ...string) config.Config {
	cfg := config.Config{}
	cfg.Access.Tailscale.Tailnet = "example.ts.net"
	cfg.Access.Tailscale.Tags = tags
	return cfg
}

func TestMatchTailscaleDeviceReturnsMeshIdentityForTaggedHost(t *testing.T) {
	ts := tailscaleDevicesServer(t, `{"devices":[
		{"id":"d1","nodeId":"node-1","name":"demo-web.tail.ts.net","hostname":"demo-web","addresses":["100.64.0.10"],"tags":["tag:serverpro-demo"]},
		{"id":"d2","nodeId":"node-2","name":"other.tail.ts.net","hostname":"other","tags":["tag:serverpro-demo"]}
	]}`)
	client := tailscale.NewWithHTTP("token", "example.ts.net", ts.URL, ts.Client())
	got, err := MatchTailscaleDevice(context.Background(), client, Candidate{Name: "demo-web"}, tailscaleMatchConfig("tag:serverpro-demo"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Tailnet != "example.ts.net" || got.NodeID != "node-1" || got.Name != "demo-web.tail.ts.net" || len(got.IPs) != 1 || got.IPs[0] != "100.64.0.10" {
		t.Fatalf("device = %+v", got)
	}
}

func TestMatchTailscaleDeviceFallsBackToHostnameWhenTagsDrift(t *testing.T) {
	ts := tailscaleDevicesServer(t, `{"devices":[
		{"id":"d1","nodeId":"node-1","name":"demo-web","hostname":"demo-web","tags":["tag:legacy"]}
	]}`)
	client := tailscale.NewWithHTTP("token", "example.ts.net", ts.URL, ts.Client())
	got, err := MatchTailscaleDevice(context.Background(), client, Candidate{Name: "demo-web"}, tailscaleMatchConfig("tag:serverpro-demo"))
	if err != nil {
		t.Fatalf("fallback should match on hostname despite tag drift: %v", err)
	}
	if got.NodeID != "node-1" {
		t.Fatalf("device = %+v", got)
	}
}

func TestMatchTailscaleDeviceUsesComputeNameWhenCandidateNameEmpty(t *testing.T) {
	ts := tailscaleDevicesServer(t, `{"devices":[{"id":"d1","nodeId":"node-1","name":"demo-web","hostname":"demo-web","tags":[]}]}`)
	client := tailscale.NewWithHTTP("token", "example.ts.net", ts.URL, ts.Client())
	cfg := tailscaleMatchConfig()
	cfg.Compute.Name = "demo-web"
	got, err := MatchTailscaleDevice(context.Background(), client, Candidate{}, cfg)
	if err != nil || got.NodeID != "node-1" {
		t.Fatalf("compute-name fallback failed: got=%+v err=%v", got, err)
	}
}

func TestMatchTailscaleDeviceReportsNotFound(t *testing.T) {
	ts := tailscaleDevicesServer(t, `{"devices":[{"id":"d1","name":"unrelated","hostname":"unrelated"}]}`)
	client := tailscale.NewWithHTTP("token", "example.ts.net", ts.URL, ts.Client())
	_, err := MatchTailscaleDevice(context.Background(), client, Candidate{Name: "demo-web"}, tailscaleMatchConfig())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestMatchTailscaleDeviceRejectsAmbiguousHosts(t *testing.T) {
	ts := tailscaleDevicesServer(t, `{"devices":[
		{"id":"d1","nodeId":"n1","name":"demo-web","hostname":"demo-web"},
		{"id":"d2","nodeId":"n2","name":"demo-web","hostname":"demo-web"}
	]}`)
	client := tailscale.NewWithHTTP("token", "example.ts.net", ts.URL, ts.Client())
	_, err := MatchTailscaleDevice(context.Background(), client, Candidate{Name: "demo-web"}, tailscaleMatchConfig())
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestMatchTailscaleDeviceWrapsListError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer ts.Close()
	client := tailscale.NewWithHTTP("token", "example.ts.net", ts.URL, ts.Client())
	_, err := MatchTailscaleDevice(context.Background(), client, Candidate{Name: "demo-web"}, tailscaleMatchConfig())
	if err == nil || !strings.Contains(err.Error(), "tailscale device list failed") {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
}

func cloudflareTunnelServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/cfd_tunnel") {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(handlerErr.Check)
	t.Cleanup(ts.Close)
	return ts
}

func TestMatchCloudflareTunnelReturnsTunnelByName(t *testing.T) {
	ts := cloudflareTunnelServer(t, `{"result":[
		{"id":"tun-1","name":"demo-web"},
		{"id":"tun-2","name":"other"}
	],"result_info":{"page":1,"total_pages":1}}`)
	client := cloudflare.NewWithHTTP("token", "acc", ts.URL, ts.Client())
	got, err := MatchCloudflareTunnel(context.Background(), client, Candidate{Name: "demo-web"}, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got.TunnelID != "tun-1" || got.Name != "demo-web" || got.Provenance != state.CloudflareTunnelImported {
		t.Fatalf("tunnel = %+v", got)
	}
}

func TestMatchCloudflareTunnelReturnsEmptyWhenNoMatch(t *testing.T) {
	ts := cloudflareTunnelServer(t, `{"result":[{"id":"tun-2","name":"other"}],"result_info":{"page":1,"total_pages":1}}`)
	client := cloudflare.NewWithHTTP("token", "acc", ts.URL, ts.Client())
	got, err := MatchCloudflareTunnel(context.Background(), client, Candidate{Name: "demo-web"}, config.Config{})
	if err != nil {
		t.Fatalf("missing tunnel is not an error: %v", err)
	}
	if got.TunnelID != "" || got.Name != "" {
		t.Fatalf("expected empty state, got %+v", got)
	}
}

func TestMatchCloudflareTunnelRejectsAmbiguousNames(t *testing.T) {
	ts := cloudflareTunnelServer(t, `{"result":[
		{"id":"tun-1","name":"demo-web"},
		{"id":"tun-2","name":"demo-web"}
	],"result_info":{"page":1,"total_pages":1}}`)
	client := cloudflare.NewWithHTTP("token", "acc", ts.URL, ts.Client())
	_, err := MatchCloudflareTunnel(context.Background(), client, Candidate{Name: "demo-web"}, config.Config{})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestMatchCloudflareTunnelFallsBackToConfigTunnelName(t *testing.T) {
	ts := cloudflareTunnelServer(t, `{"result":[{"id":"tun-9","name":"demo-web"}],"result_info":{"page":1,"total_pages":1}}`)
	client := cloudflare.NewWithHTTP("token", "acc", ts.URL, ts.Client())
	cfg := config.Config{}
	cfg.Cloudflare.Tunnel.Name = "demo-web"
	got, err := MatchCloudflareTunnel(context.Background(), client, Candidate{}, cfg)
	if err != nil || got.TunnelID != "tun-9" {
		t.Fatalf("config tunnel-name fallback failed: got=%+v err=%v", got, err)
	}
}

func TestMatchCloudflareTunnelWrapsListError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer ts.Close()
	client := cloudflare.NewWithHTTP("token", "acc", ts.URL, ts.Client())
	_, err := MatchCloudflareTunnel(context.Background(), client, Candidate{Name: "demo-web"}, config.Config{})
	if err == nil || !strings.Contains(err.Error(), "cloudflare tunnel list failed") {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
}
