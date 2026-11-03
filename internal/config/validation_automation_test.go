package config

import (
	"strings"
	"testing"
)

const (
	automationTestSocket = "/var/run/serverpro-automation/tailscaled.sock"
	automationTestCLI    = "/opt/homebrew/bin/tailscale"
)

func automationConfig() Config {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Access.Tailscale.Automation = TailscaleAutomation{Enabled: true, Socket: automationTestSocket, CLI: automationTestCLI}
	return cfg
}

// Policy writes and doctor both derive the accept source from this method,
// so it must name the fixed tag only when automation is on, and nothing
// when it is off even if stale paths remain.
func TestTailscaleAutomationSource(t *testing.T) {
	on := automationConfig().Access.Tailscale
	if got := on.AutomationSource(); got != "tag:serverpro-automation" {
		t.Fatalf("enabled source = %q", got)
	}
	off := on
	off.Automation.Enabled = false
	if got := off.AutomationSource(); got != "" {
		t.Fatalf("disabled source = %q", got)
	}
}

func TestValidateAcceptsAutomationConfig(t *testing.T) {
	if err := automationConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	off := automationConfig()
	off.Access.Tailscale.Automation = TailscaleAutomation{}
	if err := off.Validate(); err != nil {
		t.Fatalf("omitted automation section must stay valid: %v", err)
	}
}

// The paths are pasted into an ssh ProxyCommand that /bin/sh parses, so any
// shell metacharacter, relative or unclean path must be refused before it
// can reach a shell. Each case is one way to change the command.
func TestValidateRejectsUnsafeAutomationPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		socket string
		cli    string
		field  string
	}{
		{name: "empty socket", socket: "", cli: automationTestCLI, field: "socket"},
		{name: "relative socket", socket: "run/tailscaled.sock", cli: automationTestCLI, field: "socket"},
		{name: "unclean socket", socket: "/var/run/../tmp/tailscaled.sock", cli: automationTestCLI, field: "socket"},
		{name: "trailing slash socket", socket: "/var/run/", cli: automationTestCLI, field: "socket"},
		{name: "space in socket", socket: "/var/run/tail scaled.sock", cli: automationTestCLI, field: "socket"},
		{name: "empty cli", socket: automationTestSocket, cli: "", field: "cli"},
		{name: "relative cli", socket: automationTestSocket, cli: "tailscale", field: "cli"},
		{name: "command substitution cli", socket: automationTestSocket, cli: "/usr/bin/$(id)", field: "cli"},
		{name: "separator cli", socket: automationTestSocket, cli: "/usr/bin/tailscale;rm", field: "cli"},
		{name: "quote cli", socket: automationTestSocket, cli: "/usr/bin/tail'scale", field: "cli"},
		{name: "non-ascii cli", socket: automationTestSocket, cli: "/usr/bin/tailscalé", field: "cli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := automationConfig()
			cfg.Access.Tailscale.Automation.Socket = tc.socket
			cfg.Access.Tailscale.Automation.CLI = tc.cli
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "access.tailscale.automation."+tc.field+" must be an absolute path") {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

// With automation off the paths may stay in config, so a revoke is a one-key
// edit. They are still checked, so turning automation back on later cannot
// bring an unsafe path into the ProxyCommand.
func TestValidateChecksAutomationPathsWhenDisabled(t *testing.T) {
	for _, automation := range []TailscaleAutomation{{Socket: automationTestSocket}, {CLI: automationTestCLI}, {Socket: automationTestSocket, CLI: automationTestCLI}} {
		cfg := automationConfig()
		cfg.Access.Tailscale.Automation = automation
		if err := cfg.Validate(); err != nil {
			t.Fatalf("automation=%+v Validate() error = %v", automation, err)
		}
		if cfg.Access.Tailscale.AutomationSource() != "" {
			t.Fatalf("paths alone must not turn automation on: %+v", automation)
		}
	}
	for field, automation := range map[string]TailscaleAutomation{"socket": {Socket: "run/tailscaled.sock"}, "cli": {CLI: "/usr/bin/$(id)"}} {
		cfg := automationConfig()
		cfg.Access.Tailscale.Automation = automation
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "access.tailscale.automation."+field+" must be an absolute path") {
			t.Fatalf("automation=%+v Validate() error = %v", automation, err)
		}
	}
}

// A server that carried the automation tag would be an accept source for its
// own namespace peers, so the tag is refused in server tags whether or not
// automation is on. Namespace "automation" hits this through its own
// default tag.
func TestValidateRejectsAutomationTagAsServerTag(t *testing.T) {
	listed := automationConfig()
	listed.Access.Tailscale.Tags = append(listed.Access.Tailscale.Tags, AutomationTailscaleTag)
	off := listed
	off.Access.Tailscale.Automation = TailscaleAutomation{}
	clash := Example("automation")
	clash.Cloudflare.AccountID = "acc"
	for name, cfg := range map[string]Config{"automation on": listed, "automation off": off, "namespace automation": clash} {
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must not include "+AutomationTailscaleTag) {
			t.Fatalf("%s: Validate() error = %v", name, err)
		}
	}
}

// The accept rule names the admin user, so root as the admin user would let
// the automation node reach root with no browser login. Validation must stop
// that config before any policy write.
func TestValidateRejectsRootAdminWithAutomation(t *testing.T) {
	cfg := automationConfig()
	cfg.Admin.Username = "root"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "admin.username must not be root") {
		t.Fatalf("err = %v", err)
	}
}
