package bootstraptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiLockManifestAndDoctor(t *testing.T) {
	manifest := map[string]string{}
	for _, pair := range manifestEnvPairs() {
		manifest[pair[0]] = pair[1]
	}
	sum := sha256.Sum256(piLockJSON)
	for name, want := range map[string]string{
		piLockSHA256Env: hex.EncodeToString(sum[:]),
		piBinEnv:        piLock.bin,
		"SERVERPRO_BOOTSTRAP_PI_INSTALL_DIRECTORY": PiInstallDirectory,
	} {
		if got := manifest[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	assertCheck(t, Checks("deploy"), "pi "+PiVersion, piLock.sha256)
	for _, marker := range []string{"npm ci --omit=dev --ignore-scripts", "audit --omit=dev", "write_pi_project", "pi_install_commands"} {
		if !strings.Contains(installScript, marker) {
			t.Errorf("Pi install missing %q", marker)
		}
	}
}

// The embedded lock decides what every host installs, so anything short of an
// exact, integrity-pinned, registry-only production tree must be refused.
func TestLoadPiLockRejectsUnreviewedTrees(t *testing.T) {
	piKey := "node_modules/" + PiToolName
	for name, mutate := range map[string]func(lock map[string]any){
		"wrong-pi-version": func(lock map[string]any) { entry(lock, piKey)["version"] = "0.0.1" },
		"dev-package":      func(lock map[string]any) { entry(lock, "node_modules/undici")["dev"] = true },
		"other-registry": func(lock map[string]any) {
			entry(lock, "node_modules/undici")["resolved"] = "https://registry.example.invalid/undici.tgz"
		},
		"weak-integrity": func(lock map[string]any) { entry(lock, "node_modules/undici")["integrity"] = "sha1-AAAA" },
		"bin-escape":     func(lock map[string]any) { entry(lock, piKey)["bin"] = map[string]string{"pi": "../../evil.js"} },
		"path-escape": func(lock map[string]any) {
			lock["packages"].(map[string]any)["node_modules/../evil"] = entry(lock, "node_modules/undici")
		},
	} {
		t.Run(name, func(t *testing.T) {
			var lock map[string]any
			if err := json.Unmarshal(piLockJSON, &lock); err != nil {
				t.Fatal(err)
			}
			mutate(lock)
			data, err := json.Marshal(lock)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := loadPiLock(piPackageJSON, data); err == nil {
				t.Fatalf("unreviewed lock accepted")
			}
		})
	}
}

func entry(lock map[string]any, key string) map[string]any {
	return lock["packages"].(map[string]any)[key].(map[string]any)
}

// Manifest values reach rm, mv, and ln, so malformed paths must fail before use.
func TestPiManifestRejectsUnsafePaths(t *testing.T) {
	for name, override := range map[string]string{
		"directory-traversal": "SERVERPRO_BOOTSTRAP_PI_INSTALL_DIRECTORY=../serverpro-pi",
		"directory-option":    "SERVERPRO_BOOTSTRAP_PI_INSTALL_DIRECTORY=-rf",
		"directory-empty":     "SERVERPRO_BOOTSTRAP_PI_INSTALL_DIRECTORY=",
		"bin-traversal":       piBinEnv + "=../../evil.js",
		"bin-absolute":        piBinEnv + "=/tmp/evil.js",
		"bin-shell":           piBinEnv + "=dist/$(id).js",
		"bad-digest":          piLockSHA256Env + "=not-a-digest",
		"registry-plain-http": piRegistryEnv + "=http://registry.npmjs.org/",
		"registry-shell":      piRegistryEnv + "=https://registry.npmjs.org/$(id)/",
		"registry-option":     piRegistryEnv + "=--userconfig=/tmp/evil",
	} {
		t.Run(name, func(t *testing.T) {
			code, _, _ := runHelper(t, `pi_check_command "$TEST_NODE_VERSION" "$TEST_PI_VERSION" >/dev/null && pi_install_commands "$TEST_NODE_VERSION" /nonexistent >/dev/null`, append(manifestEnv(), override, "TEST_NODE_VERSION="+NodeVersion, "TEST_PI_VERSION="+PiVersion)...)
			if code == 0 {
				t.Fatalf("unsafe manifest value accepted: %q", override)
			}
		})
	}
}

// A corrupt manifest must stop before npm reads the project.
func TestWritePiProjectRejectsDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runHelper(t, `write_pi_project "$TEST_DIR"`, append(manifestEnv(), "TEST_DIR="+dir, piLockSHA256Env+"="+strings.Repeat("0", 64))...)
	if code == 0 || !strings.Contains(stderr, "digest mismatch") {
		t.Fatalf("digest mismatch accepted: exit=%d %s", code, stderr)
	}
	code, _, stderr = runHelper(t, `write_pi_project "$TEST_DIR"`, append(manifestEnv(), "TEST_DIR="+dir)...)
	if code != 0 {
		t.Fatalf("reviewed project rejected: %s", stderr)
	}
	written, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil || string(written) != string(piLockJSON) {
		t.Fatalf("written lock differs from the embedded lock: %v", err)
	}
}

// The Pi CLI version alone cannot prove the dependency tree, so bootstrap
// readiness and doctor must both reject every drift from the reviewed lock.
func TestPiCheckRejectsDriftFromReviewedLock(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js unavailable; real lock probe requires Node.js")
	}
	piKey := "node_modules/" + PiToolName
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, nodeRoot, piRoot string)
		wantOK bool
	}{
		{name: "reviewed", mutate: func(*testing.T, string, string) {}, wantOK: true},
		{name: "dependency-drift", mutate: func(t *testing.T, _, piRoot string) {
			writeJSON(t, filepath.Join(piRoot, "node_modules/undici/package.json"), map[string]string{"version": "8.10.2"})
		}},
		{name: "dependency-missing", mutate: func(t *testing.T, _, piRoot string) {
			removeAll(t, filepath.Join(piRoot, "node_modules/undici"))
		}},
		{name: "lock-drift", mutate: func(t *testing.T, _, piRoot string) {
			appendFile(t, filepath.Join(piRoot, "package-lock.json"), "\n")
		}},
		{name: "foreign-link", mutate: func(t *testing.T, nodeRoot, _ string) {
			other := filepath.Join(nodeRoot, "lib/node_modules", PiToolName, piLock.bin)
			writeExecutable(t, other, "#!/bin/sh\necho "+PiVersion+"\n")
			removeAll(t, filepath.Join(nodeRoot, "bin/pi"))
			if err := os.Symlink(other, filepath.Join(nodeRoot, "bin/pi")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong-cli-version", mutate: func(t *testing.T, _, piRoot string) {
			writeExecutable(t, filepath.Join(piRoot, piKey, piLock.bin), "#!/bin/sh\necho 0.87.1\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			nodeRoot := filepath.Join(home, ".local/share/mise/installs/node", NodeVersion)
			piRoot := filepath.Join(nodeRoot, "lib", PiInstallDirectory)
			writePiFixture(t, home, nodeRoot, piRoot)
			tc.mutate(t, nodeRoot, piRoot)
			path := filepath.Join(nodeRoot, "bin") + ":" + os.Getenv("PATH")
			env := append(manifestEnv(), "FAKE_HOME="+home, "NODE_BIN="+node, "PATH="+path, "TEST_NODE_VERSION="+NodeVersion, "TEST_PI_VERSION="+PiVersion)
			code, _, stderr := runHelper(t, `
TARGET_USER=deploy
run_as_target() { HOME="$FAKE_HOME" bash -c "$1"; }
run_as_target "set -euo pipefail; $(pi_check_command "$TEST_NODE_VERSION" "$TEST_PI_VERSION")"
`, env...)
			if (code == 0) != tc.wantOK {
				t.Fatalf("bootstrap check exit=%d, want success=%v: %s", code, tc.wantOK, stderr)
			}
			// Doctor runs the Go-built command; it must reach the same verdict.
			doctor := exec.Command("bash", "-s")
			doctor.Stdin = strings.NewReader("set -euo pipefail; " + piCheckCommand())
			doctor.Env = append(os.Environ(), "HOME="+home, "NODE_BIN="+node, "PATH="+path)
			if out, err := doctor.CombinedOutput(); (err == nil) != tc.wantOK {
				t.Fatalf("doctor check err=%v, want success=%v: %s", err, tc.wantOK, out)
			}
		})
	}
}

// writePiFixture lays out the tree `npm ci` leaves for the embedded lock:
// metadata only, with dependency entry points that throw, so the checks prove
// they never run dependency code.
func writePiFixture(t *testing.T, home, nodeRoot, piRoot string) {
	t.Helper()
	var lock piLockFile
	if err := json.Unmarshal(piLockJSON, &lock); err != nil {
		t.Fatal(err)
	}
	for path, e := range lock.Packages {
		if path == "" {
			continue
		}
		writeJSON(t, filepath.Join(piRoot, path, "package.json"), map[string]string{"version": e.Version, "main": "index.js"})
		if err := os.WriteFile(filepath.Join(piRoot, path, "index.js"), []byte("throw new Error('dependency code must not execute during verification');"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(piRoot, "package-lock.json"), piLockJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(piRoot, "node_modules", PiToolName, piLock.bin)
	writeExecutable(t, cli, "#!/bin/sh\necho "+PiVersion+"\n")
	if err := os.MkdirAll(filepath.Join(nodeRoot, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("../lib", PiInstallDirectory, "node_modules", PiToolName, piLock.bin), filepath.Join(nodeRoot, "bin/pi")); err != nil {
		t.Fatal(err)
	}
	// Fake mise runs node through the real binary and anything else as given.
	writeExecutable(t, filepath.Join(home, ".local/bin/mise"), `#!/bin/sh
set -eu
test "$1" = exec
test "$2" = --
shift 2
if test "$1" = node; then shift; exec "$NODE_BIN" "$@"; fi
exec "$@"
`)
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
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

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func removeAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
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
	if !strings.Contains(string(commands), "npm ci --omit=dev --ignore-scripts") {
		t.Fatalf("Pi install did not use the reviewed lock: %s", commands)
	}
	if strings.Contains(string(commands), "integration install pi") {
		t.Fatalf("integration proceeded after audit failure: %s", commands)
	}
}
