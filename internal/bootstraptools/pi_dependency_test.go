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
	reviewedBraceVersion    = "5.0.12"
	reviewedBraceIntegrity  = "sha512-YovQ3rzhaLMIrDjNDMkNS01tea93qhEhG5xy8f6+R0l+dw3Ki+5sCoIoI942iuLZTHWogWktgwVDhU09iNEimQ=="
	reviewedUndiciVersion   = "8.12.3"
	reviewedUndiciIntegrity = "sha512-+fdndUTwBP86cVjihTp21hZHo7d6oj1c/47XPA3lL8mM0XHs+C4EkcjmgRsFnFk2f/UONGDy9ItBItmXsLOQHw=="
	// Versions Pi 0.87.1 shrinkwraps upstream, which the repair must replace.
	vulnerableBraceVersion  = "5.0.9"
	vulnerableUndiciVersion = "8.10.2"
)

func TestPiDependencyRepairManifestAndDoctor(t *testing.T) {
	manifest := map[string]string{}
	for _, pair := range manifestEnvPairs() {
		manifest[pair[0]] = pair[1]
	}
	for name, want := range map[string]string{
		piDependencyRepairsEnv: "brace-expansion|" + reviewedBraceVersion + "|" + reviewedBraceIntegrity + "|minimatch\n" +
			"undici|" + reviewedUndiciVersion + "|" + reviewedUndiciIntegrity + "|-",
		"SERVERPRO_BOOTSTRAP_PI_DEPENDENCY_MIN_RELEASE_AGE_DAYS": "7",
	} {
		if got := manifest[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, sub := range []string{reviewedBraceVersion, reviewedBraceIntegrity, reviewedUndiciVersion, reviewedUndiciIntegrity} {
		assertCheck(t, Checks("deploy"), "pi "+PiVersion, sub)
	}
	for _, marker := range []string{"--omit=dev --save-exact --min-release-age=", "audit --omit=dev", "pi_dependency_check_command", "pi_dependency_install_specs"} {
		if !strings.Contains(installScript, marker) {
			t.Errorf("Pi repair missing %q", marker)
		}
	}
	code, stdout, stderr := runHelper(t, `pi_dependency_install_specs`, manifestEnv()...)
	if want := "brace-expansion@" + reviewedBraceVersion + " undici@" + reviewedUndiciVersion; code != 0 || stdout != want {
		t.Fatalf("install specs exit=%d stdout=%q want %q: %s", code, stdout, want, stderr)
	}
}

// Manifest values reach npm and node argv, so malformed rows must fail before use.
func TestPiDependencyRepairManifestRejectsUnsafeRows(t *testing.T) {
	for name, row := range map[string]string{
		"empty":            "",
		"missing-field":    "undici|" + reviewedUndiciVersion + "|" + reviewedUndiciIntegrity,
		"extra-field":      "undici|" + reviewedUndiciVersion + "|" + reviewedUndiciIntegrity + "|-|x",
		"bad-integrity":    "undici|" + reviewedUndiciVersion + "|sha512-short|-",
		"option-package":   "--registry=https://example.invalid|" + reviewedUndiciVersion + "|" + reviewedUndiciIntegrity + "|-",
		"option-resolve":   "undici|" + reviewedUndiciVersion + "|" + reviewedUndiciIntegrity + "|--require",
		"shell-in-spec":    "undici;id|" + reviewedUndiciVersion + "|" + reviewedUndiciIntegrity + "|-",
		"shell-in-version": "undici|$(id)|" + reviewedUndiciIntegrity + "|-",
	} {
		t.Run(name, func(t *testing.T) {
			code, _, _ := runHelper(t, `pi_dependency_install_specs`, append(manifestEnv(), piDependencyRepairsEnv+"="+row)...)
			if code == 0 {
				t.Fatalf("unsafe repair row accepted: %q", row)
			}
		})
	}
}

// piDependencyFixture is one shrinkwrapped package as installed and as locked.
type piDependencyFixture struct {
	installed, locked, integrity string
}

// An exact Pi version is insufficient when its shrinkwrap retains a vulnerable dependency.
func TestPiDependencyProbeRejectsUnrepairedGraph(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js unavailable; real dependency probe requires Node.js")
	}
	patchedBrace := piDependencyFixture{installed: reviewedBraceVersion, locked: reviewedBraceVersion, integrity: reviewedBraceIntegrity}
	patchedUndici := piDependencyFixture{installed: reviewedUndiciVersion, locked: reviewedUndiciVersion, integrity: reviewedUndiciIntegrity}
	for _, tc := range []struct {
		name                string
		brace, undici       piDependencyFixture
		missingLock, wantOK bool
	}{
		{name: "patched", brace: patchedBrace, undici: patchedUndici, wantOK: true},
		{name: "brace-vulnerable", brace: piDependencyFixture{vulnerableBraceVersion, vulnerableBraceVersion, reviewedBraceIntegrity}, undici: patchedUndici},
		{name: "brace-lock-drift", brace: piDependencyFixture{reviewedBraceVersion, vulnerableBraceVersion, reviewedBraceIntegrity}, undici: patchedUndici},
		{name: "brace-integrity-drift", brace: piDependencyFixture{reviewedBraceVersion, reviewedBraceVersion, "sha512-wrong"}, undici: patchedUndici},
		{name: "undici-vulnerable", brace: patchedBrace, undici: piDependencyFixture{vulnerableUndiciVersion, vulnerableUndiciVersion, reviewedUndiciIntegrity}},
		{name: "undici-lock-drift", brace: patchedBrace, undici: piDependencyFixture{reviewedUndiciVersion, vulnerableUndiciVersion, reviewedUndiciIntegrity}},
		{name: "undici-integrity-drift", brace: patchedBrace, undici: piDependencyFixture{reviewedUndiciVersion, reviewedUndiciVersion, "sha512-wrong"}},
		{name: "missing-lock", brace: patchedBrace, undici: patchedUndici, missingLock: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, ".local/share/mise/installs/node", NodeVersion, "lib/node_modules", PiToolName)
			metadata := map[string]any{
				"package.json":                              map[string]string{},
				"node_modules/minimatch/package.json":       map[string]string{"name": "minimatch", "main": "index.js"},
				"node_modules/brace-expansion/package.json": map[string]string{"name": "brace-expansion", "version": tc.brace.installed},
				"node_modules/undici/package.json":          map[string]string{"name": "undici", "version": tc.undici.installed, "main": "index.js"},
			}
			if !tc.missingLock {
				metadata["npm-shrinkwrap.json"] = map[string]any{"packages": map[string]any{
					"node_modules/brace-expansion": map[string]string{"version": tc.brace.locked, "integrity": tc.brace.integrity},
					"node_modules/undici":          map[string]string{"version": tc.undici.locked, "integrity": tc.undici.integrity},
				}}
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
			// Dependency entry points throw so the probe proves it reads metadata only.
			for _, entry := range []string{"node_modules/minimatch/index.js", "node_modules/undici/index.js"} {
				if err := os.WriteFile(filepath.Join(root, entry), []byte("throw new Error('dependency code must not execute during verification');"), 0o600); err != nil {
					t.Fatal(err)
				}
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
			// Doctor runs the Go-built command; it must reach the same verdict.
			doctor := exec.Command("bash", "-s")
			doctor.Stdin = strings.NewReader(piDependencyCheckCommand())
			doctor.Env = append(os.Environ(), "HOME="+home, "NODE_BIN="+node)
			if out, err := doctor.CombinedOutput(); (err == nil) != tc.wantOK {
				t.Fatalf("doctor probe err=%v, want success=%v: %s", err, tc.wantOK, out)
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
