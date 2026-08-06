package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEd25519KeyConvergenceHandlesFreshRerunAndMissingPublicKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen unavailable")
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	script := `set -eu
TARGET_USER=fixture
TARGET_GID=fixture
ssh_dir=$(dirname "${TEST_KEY_PATH}")
key_path="${TEST_KEY_PATH}"
runuser() {
  shift
  shift
  if [ "${1:-}" = -- ]; then shift; fi
  "$@"
}
chown() { :; }
` + ed25519KeyConvergenceScript("serverpro fixture key")
	run := func() {
		t.Helper()
		cmd := exec.Command("bash", "-c", script)
		cmd.Env = append(os.Environ(), "TEST_KEY_PATH="+keyPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("key convergence failed: %v: %s", err, out)
		}
	}

	run()
	privateBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	for path, wantMode := range map[string]os.FileMode{keyPath: 0o600, keyPath + ".pub": 0o644} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != wantMode {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), wantMode)
		}
	}

	run()
	privateAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(privateAfter) != string(privateBefore) {
		t.Fatal("rerun replaced existing private key")
	}

	if err := os.Remove(keyPath + ".pub"); err != nil {
		t.Fatal(err)
	}
	run()
	publicKey, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(publicKey), "ssh-ed25519 ") {
		t.Fatalf("regenerated public key = %q", publicKey)
	}
}
