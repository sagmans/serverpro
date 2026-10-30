package network

import (
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestLockdownScriptAllowsCloudflareTunnelEgress(t *testing.T) {
	cfg := config.Example("prod")
	script := LockdownScript(cfg)
	for _, want := range []string{"ufw allow out 443/tcp", "ufw allow out 22/tcp", "ufw allow out 7844/tcp", "ufw allow out 7844/udp", "ufw allow out 41641/udp"} {
		if !strings.Contains(script, want) {
			t.Fatalf("lockdown script missing %q\n%s", want, script)
		}
	}
}

func TestLockdownScriptAppliesEveryAllowRuleAndProvesTheResult(t *testing.T) {
	cfg := config.Example("prod")
	script := LockdownScript(cfg)
	rules := RestrictedAllowOut()
	for _, rule := range rules {
		if !strings.Contains(script, "ufw allow out "+rule+"\n") {
			t.Fatalf("lockdown script missing rule %q\n%s", rule, script)
		}
	}
	// Doctor checks the rules through this accessor, so a caller must not be
	// able to change what the lockdown applies.
	rules[0] = "1/tcp"
	if RestrictedAllowOut()[0] == "1/tcp" {
		t.Fatal("RestrictedAllowOut exposes the lockdown's own list")
	}
	for _, want := range []string{"set -eu\n", "deny|reject", "/dev/tcp/" + EgressBlockedProbeHost, "/dev/tcp/" + egressAllowedProbeHost, LockdownFailedPrefix} {
		if !strings.Contains(script, want) {
			t.Fatalf("restricted lockdown missing its proof %q\n%s", want, script)
		}
	}
	cfg.Network.Egress.Mode = config.EgressModeOpen
	if script := LockdownScript(cfg); strings.Contains(script, "deny outgoing") || strings.Contains(script, LockdownFailedPrefix) {
		t.Fatalf("open egress must not lock down:\n%s", script)
	}
}
