package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestRemoteChecksIncludesRestrictedEgressWarning(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Egress.Mode = "restricted"
	results := remoteChecksWithOptions(context.Background(), cfg, &fakeRemote{}, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "egress precision", Warn, "best-effort") {
		t.Fatalf("missing restricted egress warning: %+v", results)
	}
}

func TestRemoteChecksSkipsCloudflaredWhenNoIngress(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "none"
	r := &scriptedRemote{responses: map[string][]remoteCall{}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	for _, res := range results {
		if res.Name == "cloudflared" {
			t.Fatalf("cloudflared check should be skipped when ingress=none: %+v", results)
		}
	}
}

func TestRemoteChecksIncludesCloudflaredWhenIngressConfigured(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "cloudflare-tunnel"
	r := &scriptedRemote{responses: map[string][]remoteCall{
		"systemctl is-active cloudflared": {{out: "active\n"}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "cloudflared", Pass, "active") {
		t.Fatalf("expected cloudflared pass: %+v", results)
	}
}

func TestRemoteChecksEgressCommandUsesReliableEndpoints(t *testing.T) {
	cfg := config.Example("prod")
	r := &scriptedRemote{responses: map[string][]remoteCall{}}
	_ = remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	for _, call := range r.commands {
		if strings.Contains(call, "www.cloudflare.com") {
			t.Fatalf("egress check should not use www.cloudflare.com: %q", call)
		}
		if strings.Contains(call, "1.1.1.1") {
			return
		}
	}
	t.Fatal("egress check should include 1.1.1.1")
}
