package doctor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestSSHDHardeningExpectationsAreExplicit(t *testing.T) {
	want := map[string]string{
		"PermitRootLogin":                 "no",
		"PasswordAuthentication":          "no",
		"KbdInteractiveAuthentication":    "no",
		"ChallengeResponseAuthentication": "no",
		"X11Forwarding":                   "no",
		"AllowAgentForwarding":            "no",
		"AllowTcpForwarding":              "no",
		"PermitTunnel":                    "no",
		"PermitOpen":                      "none",
	}
	if !reflect.DeepEqual(sshdHardeningExpectations, want) {
		t.Fatalf("sshd hardening expectations = %#v, want %#v", sshdHardeningExpectations, want)
	}
}

func TestRemoteSSHDSettingCheckRequiresExplicitExpectation(t *testing.T) {
	result := remoteSSHDSettingCheckWithOptions(context.Background(), &scriptedRemote{}, "deploy", "prod-01", "sshd future setting", "FutureSetting", Options{})
	if result.Status != Fail || !strings.Contains(result.Evidence, "missing sshd hardening expectation") {
		t.Fatalf("expected missing expectation failure, got %+v", result)
	}
}

func TestRemoteChecksUseSSHDConfigFallback(t *testing.T) {
	cfg := config.Example("prod")
	r := &fakeRemote{}
	_ = remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	for _, want := range []string{"Missing privilege separation directory: /run/sshd", "/etc/ssh/sshd_config.d/99-serverpro.conf", "permitrootlogin no", "passwordauthentication no", "kbdinteractiveauthentication no", "ChallengeResponseAuthentication no", "x11forwarding no", "allowagentforwarding no", "allowtcpforwarding no", "permittunnel no", "PermitOpen none"} {
		if !hasCommand(r.commands, want) {
			t.Fatalf("missing sshd fallback %q in %#v", want, r.commands)
		}
	}
}

func TestSSHDSettingsFixRestartsOnlyAfterValidConfig(t *testing.T) {
	command := sshdSettingsFixCommand()
	if !strings.Contains(command, "sshd -t && systemctl restart ssh") {
		t.Fatalf("sshd fix must validate config before restart:\n%s", command)
	}
	if strings.Contains(command, "sshd -t\nsystemctl restart ssh") {
		t.Fatalf("sshd fix must not restart after failed validation:\n%s", command)
	}
}

func TestRemoteChecksWithFixAppliesFailedFixableSSHDSetting(t *testing.T) {
	cfg := config.Example("prod")
	check := sshdSettingValueCommand("AllowAgentForwarding", "no")
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("allowagentforwarding yes")}, {out: "allowagentforwarding no"}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true})
	if !hasResult(Report{Results: results}, "sshd agent forwarding", Pass, "fixed") {
		t.Fatalf("missing fixed agent-forwarding result: %+v", results)
	}
	for _, want := range []string{"AllowAgentForwarding no", "AllowTcpForwarding no", "PermitTunnel no", "PermitOpen none", "systemctl restart ssh"} {
		if !hasCommand(r.commands, want) {
			t.Fatalf("missing sshd fix command %q in %#v", want, r.commands)
		}
	}
}
