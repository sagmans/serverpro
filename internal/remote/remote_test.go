package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func bootstrapInputFixture(t *testing.T, password string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out")
	installFakeSudo(t, dir, password)
	script := "read value\nprintf '%s' \"$value\" > " + testShellQuote(outPath)
	return dir, outPath, script
}

func TestSudoBootstrapExecutesScriptWithSeparateInput(t *testing.T) {
	dir, outPath, script := bootstrapInputFixture(t, "correct horse battery staple")
	cmd := testShellCommand(sudoBootstrapCommand(), dir)
	cmd.Stdin = strings.NewReader(sudoPayloadWithInput(script, "script-stdin-value", "correct horse battery staple"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "script-stdin-value" {
		t.Fatalf("script stdin = %q", got)
	}
}

func TestSudoBootstrapUsesTempScriptAndCleanup(t *testing.T) {
	bootstrap := sudoBootstrapCommand()
	if strings.Contains(bootstrap, "input_tmp") {
		t.Fatalf("bootstrap must not persist protected input in temp files\n%s", bootstrap)
	}
	for _, want := range []string{"mktemp", "chmod 600", "trap cleanup EXIT", "sudo -S -p '' -v", "sudo -n sh", "rm -f"} {
		if !strings.Contains(bootstrap, want) {
			t.Fatalf("bootstrap missing %q\n%s", want, bootstrap)
		}
	}
	for _, leaked := range []string{"correct horse battery staple", "cloudflare-token"} {
		if strings.Contains(bootstrap, leaked) {
			t.Fatalf("bootstrap leaked %q\n%s", leaked, bootstrap)
		}
	}
}

func installFakeSudo(t *testing.T, dir, password string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
if [ "$1" = "-S" ]; then
  read actual
  if [ "$actual" != "` + password + `" ]; then
    echo "bad sudo password" >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "-n" ]; then
  shift
  exec "$@"
fi
echo "unexpected sudo args: $*" >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func testShellCommand(script, pathDir string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+pathDir+":"+os.Getenv("PATH"))
	return cmd
}

func testShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
