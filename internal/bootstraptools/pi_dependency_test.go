package bootstraptools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	reviewedBraceVersion   = "5.0.12"
	reviewedBraceIntegrity = "sha512-YovQ3rzhaLMIrDjNDMkNS01tea93qhEhG5xy8f6+R0l+dw3Ki+5sCoIoI942iuLZTHWogWktgwVDhU09iNEimQ=="
)

func TestPiDependencyRepairManifestAndDoctor(t *testing.T) {
	manifest := map[string]string{}
	for _, pair := range manifestEnvPairs() {
		manifest[pair[0]] = pair[1]
	}
	for name, want := range map[string]string{
		"SERVERPRO_BOOTSTRAP_PI_BRACE_EXPANSION_VERSION":         reviewedBraceVersion,
		"SERVERPRO_BOOTSTRAP_PI_BRACE_EXPANSION_INTEGRITY":       reviewedBraceIntegrity,
		"SERVERPRO_BOOTSTRAP_PI_DEPENDENCY_MIN_RELEASE_AGE_DAYS": "7",
	} {
		if got := manifest[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	assertCheck(t, Checks("deploy"), "pi "+PiVersion, reviewedBraceVersion)
	assertCheck(t, Checks("deploy"), "pi "+PiVersion, reviewedBraceIntegrity)
	for _, marker := range []string{"--omit=dev --save-exact --min-release-age=", "audit --omit=dev", "pi_dependency_check_command"} {
		if !strings.Contains(installScript, marker) {
			t.Errorf("Pi repair missing %q", marker)
		}
	}
}

// An exact Pi version is insufficient when its shrinkwrap retains a vulnerable dependency.
func TestPiDependencyProbeRejectsUnrepairedGraph(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js unavailable; real dependency probe requires Node.js")
	}
	for _, tc := range []struct {
		name, installed, locked, integrity string
		missingLock, wantOK                bool
	}{
		{name: "patched", installed: reviewedBraceVersion, locked: reviewedBraceVersion, integrity: reviewedBraceIntegrity, wantOK: true},
		{name: "vulnerable", installed: "5.0.9", locked: "5.0.9", integrity: reviewedBraceIntegrity},
		{name: "lock-drift", installed: reviewedBraceVersion, locked: "5.0.9", integrity: reviewedBraceIntegrity},
		{name: "integrity-drift", installed: reviewedBraceVersion, locked: reviewedBraceVersion, integrity: "sha512-wrong"},
		{name: "missing-lock", installed: reviewedBraceVersion, missingLock: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, ".local/share/mise/installs/node", NodeVersion, "lib/node_modules", PiToolName)
			metadata := map[string]any{
				"package.json":                              map[string]string{},
				"node_modules/minimatch/package.json":       map[string]string{"name": "minimatch", "main": "index.js"},
				"node_modules/brace-expansion/package.json": map[string]string{"name": "brace-expansion", "version": tc.installed},
			}
			if !tc.missingLock {
				metadata["npm-shrinkwrap.json"] = map[string]any{"packages": map[string]any{"node_modules/brace-expansion": map[string]string{"version": tc.locked, "integrity": tc.integrity}}}
			}
			for name, value := range metadata {
				path := filepath.Join(root, name)
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "node_modules/minimatch/index.js"), []byte("throw new Error('dependency code must not execute during verification');"), 0o600); err != nil {
				t.Fatal(err)
			}
			mise := filepath.Join(home, ".local/bin/mise")
			if err := os.MkdirAll(filepath.Dir(mise), 0o700); err != nil {
				t.Fatal(err)
			}
			miseScript := `#!/bin/sh
set -eu
test "$1" = exec
test "$2" = --
test "$3" = node
shift 3
exec "$NODE_BIN" "$@"
`
			if err := os.WriteFile(mise, []byte(miseScript), 0o700); err != nil {
				t.Fatal(err)
			}
			env := append(manifestEnv(), "FAKE_HOME="+home, "NODE_BIN="+node, "TEST_NODE_VERSION="+NodeVersion)
			code, _, stderr := runHelper(t, `
TARGET_USER=deploy
run_as_target() { HOME="$FAKE_HOME" bash -c "$1"; }
command=$(pi_dependency_check_command "$TEST_NODE_VERSION")
run_as_target "$command"
`, env...)
			if (code == 0) != tc.wantOK {
				t.Fatalf("probe exit=%d, want success=%v: %s", code, tc.wantOK, stderr)
			}
		})
	}
}

// Audit errors must not be hidden by later reshim or integration commands.
func TestPiAuditFailureStopsIntegration(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "commands.log")
	code, _, stderr := runHelper(t, `
BOOTSTRAP_TARGET=all
TARGET_USER=deploy
TARGET_HOME=/home/deploy
TARGET_GID=1000
ensure_mise_shell_activation() { :; }
repair_mise_config_for_user() { :; }
configure_user_tools_for_target() { :; }
target_managed_mise_tool_ready() { return 0; }
target_pi_ready() { return 1; }
target_herdr_ready() { return 0; }
target_herdr_pi_integration_ready() { return 1; }
run_as_target() {
  printf '%s\n' "$1" >>"$CAPTURE"
  if [[ "$1" == *'audit --omit=dev'* ]]; then return 1; fi
}
install_user_tools_for_target all
`, append(manifestEnv(), "CAPTURE="+capture)...)
	if code == 0 {
		t.Fatalf("audit failure accepted: %s", stderr)
	}
	commands, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(commands), "integration install pi") {
		t.Fatalf("integration proceeded after audit failure: %s", commands)
	}
}
