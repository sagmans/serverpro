package network

import (
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
)

func TestLockdownScriptAllowsCloudflareTunnelEgress(t *testing.T) {
	cfg := config.Example("prod")
	script := LockdownScript(cfg)
	for _, want := range []string{"ufw allow out 443/tcp", "ufw allow out 7844/tcp", "ufw allow out 7844/udp", "ufw allow out 41641/udp"} {
		if !strings.Contains(script, want) {
			t.Fatalf("lockdown script missing %q\n%s", want, script)
		}
	}
}
