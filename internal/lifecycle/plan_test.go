package lifecycle

import (
	"bytes"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/config"
)

func TestPlanMentionsConnectorOnlyNoPublicSSH(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	var b bytes.Buffer
	if err := BuildPlan(cfg).Write(&b, false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"deny public ingress", "connector-only", "server tools", bootstraptools.DefaultToolsetDescription(), "Pi and gh authentication remain operator-owned", "doctor"} {
		if !strings.Contains(out, want) {
			t.Fatalf("plan missing %q\n%s", want, out)
		}
	}
	for _, oldTerm := range []string{"hetzner", "firewall", "server type"} {
		if strings.Contains(strings.ToLower(out), oldTerm) {
			t.Fatalf("plan leaked provider term %q\n%s", oldTerm, out)
		}
	}
}
