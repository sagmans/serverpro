package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/hostplatform"
)

// TestHostStateCommandRoundTripsThroughParseHostState runs the read behind
// the doctor ingress check on the unmanaged scratch host, so its answer stays
// one ParseHostState reads and an unclassifiable host still fails closed.
func TestHostStateCommandRoundTripsThroughParseHostState(t *testing.T) {
	floor := hostplatform.CloudflaredPackageBaseline().MinimumVersion
	processOutsideService := dormantHost(floor, receiptMatching)
	processOutsideService.pgrepStatus = "0"
	for _, tc := range []struct {
		name   string
		host   unmanagedHost
		want   HostState
		wantOK bool
	}{
		{name: "absent", host: unmanagedHost{load: "not-found", active: "inactive"}, want: HostStateAbsent, wantOK: true},
		{name: "active", host: activeHost(floor, receiptMatching), want: HostStateActive, wantOK: true},
		{name: "dormant", host: dormantHost(floor, receiptMatching), want: HostStateDormant, wantOK: true},
		{name: "process outside service", host: processOutsideService},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newUnmanagedHarness(t, tc.host)
			cmd := exec.Command(posixShell(), "-c", hostStateCommand(h.binaryPath, h.receiptPath))
			cmd.Env = h.env
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantOK {
				t.Fatalf("host state err=%v output=%q, want ok=%v", err, out, tc.wantOK)
			}
			state, ok := ParseHostState(string(out))
			if ok != tc.wantOK || state != tc.want {
				t.Fatalf("ParseHostState(%q) = %q, %v; want %q, %v", out, state, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestHostStateCommandUsesProductionPaths keeps the doctor read on the binary
// and receipt that install and the unmanaged audit use; the scratch-host test
// above renders other paths, so it cannot catch a production path drift.
func TestHostStateCommandUsesProductionPaths(t *testing.T) {
	command := HostStateCommand()
	for _, want := range []string{cloudflaredBinaryPath, cloudflaredReceiptPath, hostStateLinePrefix} {
		if !strings.Contains(command, want) {
			t.Fatalf("host state command missing %q: %s", want, command)
		}
	}
	if out, err := exec.Command(posixShell(), "-n", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("host state command syntax: %v\n%s", err, out)
	}
}

// TestParseHostStateRejectsUnknownAnswers keeps a garbled or foreign answer
// from reading as an absent connector.
func TestParseHostStateRejectsUnknownAnswers(t *testing.T) {
	for _, out := range []string{"", "ok", hostStateLinePrefix + "running\n", "absent\n"} {
		if state, ok := ParseHostState(out); ok {
			t.Fatalf("ParseHostState(%q) = %q, want rejection", out, state)
		}
	}
	if state, ok := ParseHostState("noise\n" + HostStateAnswer(HostStateDormant)); !ok || state != HostStateDormant {
		t.Fatalf("ParseHostState(answer) = %q, %v", state, ok)
	}
}

// TestPinCheckCommandRequiresExactServerproPin covers the edits the connector
// check cannot see: a deleted pin and a pin whose priority was lowered.
func TestPinCheckCommandRequiresExactServerproPin(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		write, wantOK bool
		wantOut       string
	}{
		{name: "serverpro pin", content: cloudflaredAPTPreferences(), write: true, wantOK: true, wantOut: "priority 990"},
		{name: "lowered priority", content: strings.Replace(cloudflaredAPTPreferences(), "990", "500", 1), write: true, wantOut: "differs"},
		{name: "missing pin", wantOut: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinPath := filepath.Join(t.TempDir(), "serverpro-cloudflared")
			if tc.write {
				if err := os.WriteFile(pinPath, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := strings.ReplaceAll(PinCheckCommand(), cloudflaredAPTPreferencesPath, pinPath)
			out, err := exec.Command("sh", "-c", command).CombinedOutput()
			if (err == nil) != tc.wantOK || !strings.Contains(string(out), tc.wantOut) {
				t.Fatalf("pin check err=%v output=%q, want ok=%v containing %q", err, out, tc.wantOK, tc.wantOut)
			}
		})
	}
}

// TestPinRepairScriptWritesOnlyThePin keeps the repair safe on a healthy
// connector: it must not reach apt, the service, or the tunnel token.
func TestPinRepairScriptWritesOnlyThePin(t *testing.T) {
	script := PinRepairScript()
	body := script[strings.Index(script, "main() {"):]
	body = body[:strings.Index(body, "\n}\n")]
	if !strings.Contains(body, "require_supported_host") || !strings.Contains(body, "write_cloudflared_apt_pin") {
		t.Fatalf("repair main lacks host gate or pin write:\n%s", body)
	}
	for _, forbidden := range []string{"apt_update", "install_cloudflared", "systemctl", "token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("pin repair reaches %q:\n%s", forbidden, body)
		}
	}
}
