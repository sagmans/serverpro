package doctor

import (
	"context"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestRemoteCheckEvidenceSummariesStayConcise(t *testing.T) {
	cfg := config.Example("prod")
	r := &scriptedRemote{responses: map[string][]remoteCall{
		"cloud-init status --wait":                       {{out: "status: done\nraw detail"}},
		"ufw status verbose | grep -Fx 'Status: active'": {{out: "Status: active\nLANG='en_US.UTF-8'"}},
		"ss -H -tuln": {{out: "tcp LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:* users:((\"private\",pid=1))\nudp UNCONN 0 0 127.0.0.53:53 0.0.0.0:*\ntcp LISTEN 0 4096 100.64.0.1:22 0.0.0.0:*"}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "cloud-init", Pass, "status: done") {
		t.Fatalf("missing concise cloud-init evidence: %+v", results)
	}
	if !hasResult(Report{Results: results}, "ufw active", Pass, "active") || hasResult(Report{Results: results}, "ufw active", Pass, "LANG=") {
		t.Fatalf("missing concise ufw evidence or leaked env: %+v", results)
	}
	if !hasResult(Report{Results: results}, "listening ports", Pass, "3 listening sockets; public_bind=0 loopback=2 private=1 other=0") || hasResult(Report{Results: results}, "listening ports", Pass, "private\"") {
		t.Fatalf("missing concise listening port evidence or leaked process detail: %+v", results)
	}
}
