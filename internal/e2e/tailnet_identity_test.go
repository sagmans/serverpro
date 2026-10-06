//go:build serverpro_full_chain_e2e

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These journeys pin the create-time device identity rule end to end: the
// compiled binary must bind the node enrolled with its own bootstrap key and
// must never open a Tailscale SSH session when the identity is ambiguous.
const (
	compiledTailnetTwinEnv       = "SERVERPRO_E2E_TAILNET_TWIN"
	compiledTailnetTwinStale     = "stale"
	compiledTailnetTwinAmbiguous = "ambiguous"
	compiledManagedDeviceID      = "e2e-device"
	compiledTwinDeviceID         = "e2e-twin-device"
	compiledTailscaleCallLog     = "tailscale-calls.log"
	compiledTailscaleSSHPrefix   = "ssh"
)

func TestCompiledCreateBindsEnrolledDeviceOverStaleTwin(t *testing.T) {
	result, home, namespace, calls := runTwinCreate(t, "e2e-twin-stale", compiledTailnetTwinStale)
	requireSuccessJSON(t, result)
	if got := stateNodeID(t, home, namespace); got != compiledManagedDeviceID {
		t.Fatalf("recorded node_id = %q, want %q", got, compiledManagedDeviceID)
	}
	if !strings.Contains(readFile(t, calls), compiledTailscaleSSHPrefix) {
		t.Fatal("create did not reach the bound device over Tailscale SSH")
	}
}

func TestCompiledCreateFailsClosedOnAmbiguousDevice(t *testing.T) {
	result, home, namespace, calls := runTwinCreate(t, "e2e-twin-ambiguous", compiledTailnetTwinAmbiguous)
	if result.err == nil {
		t.Fatalf("create succeeded with an ambiguous tailnet identity: %s", result.stdout)
	}
	output := result.stdout + result.stderr
	if !strings.Contains(output, "ambiguous") || !strings.Contains(output, compiledTwinDeviceID) {
		t.Fatalf("ambiguity not reported with device ids: %q", output)
	}
	for _, line := range strings.Split(readFile(t, calls), "\n") {
		if strings.HasPrefix(line, compiledTailscaleSSHPrefix) {
			t.Fatalf("Tailscale SSH ran despite ambiguity: %q", line)
		}
	}
	if got := stateNodeID(t, home, namespace); got != "" {
		t.Fatalf("ambiguous create recorded node_id %q", got)
	}
}

// runTwinCreate runs one compiled create against a synthetic tailnet listing
// and returns the log of every fake tailscale CLI invocation.
func runTwinCreate(t *testing.T, namespace, mode string) (commandResult, string, string, string) {
	t.Helper()
	fixture := newProviderFixture(t)
	binary := buildE2EBinary(t)
	fakeBin, calls := writeLoggingFakeTailscale(t)
	home := t.TempDir()
	writeCredentials(t, home, namespace)
	env := replaceEnv(journeyEnv(home, fakeBin, fixture.URL(), namespace), compiledTailnetTwinEnv, mode)
	artifacts := newArtifactLog(t, namespace)
	result := runCommand(binary, env, "server", "create", testServer,
		"--namespace", namespace, "--provider", "hetzner",
		"--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04",
		"--ingress", "none", "--non-interactive", "--yes")
	artifacts.record("create", result)
	return result, home, namespace, calls
}

// writeLoggingFakeTailscale records argv for each call so a journey can prove
// whether any SSH session reached a device.
func writeLoggingFakeTailscale(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, compiledTailscaleCallLog)
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >>'" + calls + "'\ncat >/dev/null\nprintf 'ok\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, calls
}

func stateNodeID(t *testing.T, home, namespace string) string {
	t.Helper()
	path := filepath.Join(home, ".local", "state", "serverpro", "namespaces", namespace, "servers", testServer+".json")
	var st struct {
		Tailscale struct {
			NodeID string `json:"node_id"`
		} `json:"tailscale"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &st); err != nil {
		t.Fatal(err)
	}
	return st.Tailscale.NodeID
}

// readFile treats a missing file as empty: the call log exists only once the
// fake CLI has run at least once.
func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
