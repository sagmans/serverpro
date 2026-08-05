package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/mesh"
)

type fakeTailscaleDNS struct {
	cfg mesh.DNSConfig
	err error
}

func (f fakeTailscaleDNS) WaitDevice(context.Context, string, []string) (mesh.Device, error) {
	return mesh.Device{Name: "prod-01.tailnet.ts.net", Hostname: "prod-01", Online: true}, nil
}

func (f fakeTailscaleDNS) DNSConfig(context.Context) (mesh.DNSConfig, error) {
	return f.cfg, f.err
}

func TestCheckTailscaleDNS(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		client     TailscaleClient
		wantStatus Status
		wantSub    string
	}{
		{name: "no token skips", token: "", client: fakeTailscaleDNS{}, wantStatus: Skip, wantSub: "no API token"},
		{name: "nil client skips", token: "tok", client: nil, wantStatus: Skip},
		{name: "client without dns support skips", token: "tok", client: fakeTailscale{}, wantStatus: Skip, wantSub: "not supported"},
		{name: "api error warns", token: "tok", client: fakeTailscaleDNS{err: errors.New("boom")}, wantStatus: Warn, wantSub: "boom"},
		{name: "magicdns without resolvers warns", token: "tok", client: fakeTailscaleDNS{cfg: mesh.DNSConfig{MagicDNS: true}}, wantStatus: Warn, wantSub: "no global nameservers"},
		{name: "magicdns with resolvers passes", token: "tok", client: fakeTailscaleDNS{cfg: mesh.DNSConfig{MagicDNS: true, GlobalNameservers: []string{"9.9.9.9", "1.1.1.1"}}}, wantStatus: Pass, wantSub: "resolvers=2"},
		{name: "magicdns off without resolvers passes", token: "tok", client: fakeTailscaleDNS{cfg: mesh.DNSConfig{}}, wantStatus: Pass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkTailscaleDNS(context.Background(), tt.token, tt.client)
			if result.Name != "tailnet dns" || result.Scope != "provider" {
				t.Fatalf("bad identity: %+v", result)
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s (%+v)", result.Status, tt.wantStatus, result)
			}
			if tt.wantSub != "" && !strings.Contains(result.Evidence+result.Remediation, tt.wantSub) {
				t.Fatalf("missing %q in %+v", tt.wantSub, result)
			}
		})
	}
}

func TestCheckTailscaleDNSWarnRemediationGuidesToConsole(t *testing.T) {
	result := checkTailscaleDNS(context.Background(), "tok", fakeTailscaleDNS{cfg: mesh.DNSConfig{MagicDNS: true}})
	if result.Status != Warn {
		t.Fatalf("status = %s, want warn", result.Status)
	}
	if !strings.Contains(result.Remediation, "admin console") {
		t.Fatalf("remediation lacks operator guidance: %+v", result)
	}
}
