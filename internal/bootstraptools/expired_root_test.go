package bootstraptools

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// expiredRootBin fakes a host whose root password is expired, as DigitalOcean
// Ubuntu images ship: sudo invoked as root fails PAM's account stage, while
// runuser (no account stage) works. Every call is logged so tests can prove
// which tool a root-to-admin drop used.
func expiredRootBin(t *testing.T) (binDir, logPath string) {
	t.Helper()
	binDir = t.TempDir()
	logPath = filepath.Join(t.TempDir(), "calls.log")
	fakes := map[string]string{
		"id": `if [ "${1:-}" = -u ] && [ $# -eq 1 ]; then echo 0; exit 0; fi
exec /usr/bin/id "$@"`,
		"sudo": `printf 'sudo %s\n' "$*" >>"$CALL_LOG"
if [ "$(id -u)" = 0 ]; then
  echo 'sudo: Account or password is expired, reset your password and try again' >&2
  exit 1
fi
exit 1`,
		"runuser": `printf 'runuser %s\n' "$*" >>"$CALL_LOG"
[ "${1:-}" = -u ] && [ -n "${2:-}" ] && [ "${3:-}" = -- ] || { echo 'unexpected runuser usage' >&2; exit 2; }
shift 3
exec "$@"`,
		"getent": `[ "$1" = passwd ] && [ "$2" = deploy ] || exit 2
printf 'deploy:x:1000:1000::%s:/bin/bash\n' "$FAKE_HOME"`,
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return binDir, logPath
}

func readCallLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// TestRunAsTargetSurvivesExpiredRootPassword reproduces the assagmancom/webapp
// field failure: the old sudo -H -u drop fails on such a host, run_as_target
// reaches the admin user through runuser with a reset environment.
func TestRunAsTargetSurvivesExpiredRootPassword(t *testing.T) {
	binDir, logPath := expiredRootBin(t)
	home := t.TempDir()
	env := []string{"PATH=" + binDir + ":" + os.Getenv("PATH"), "CALL_LOG=" + logPath, "FAKE_HOME=" + home, "LEAK_CHECK=leaked"}

	// Control: the harness really models the failure the old drop hit.
	code, _, stderr := runHelper(t, `sudo -H -u deploy true`, env...)
	if code == 0 || !strings.Contains(stderr, "Account or password is expired") {
		t.Fatalf("fake sudo as root = %d %q, want the expired-account failure", code, stderr)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runHelper(t, `
TARGET_USER=deploy
TARGET_HOME="$FAKE_HOME"
TARGET_PATH="$PATH"
run_as_target 'printf "%s|%s|%s|%s|%s" "$USER" "$LOGNAME" "$HOME" "$PWD" "${LEAK_CHECK:-clean}"'
`, env...)
	if code != 0 {
		t.Fatalf("run_as_target on an expired-root host failed: %d (stderr: %s)", code, stderr)
	}
	if want := "deploy|deploy|" + home + "|" + home + "|clean"; stdout != want {
		t.Fatalf("run_as_target environment = %q, want %q", stdout, want)
	}
	calls := readCallLog(t, logPath)
	if !strings.Contains(calls, "runuser -u deploy -- env -i ") || strings.Contains(calls, "sudo ") {
		t.Fatalf("root-to-admin drop must use runuser, never sudo; calls:\n%s", calls)
	}
}

// TestUserHomeCommandSurvivesExpiredRootPassword covers the doctor-side drop
// that the tool checks share.
func TestUserHomeCommandSurvivesExpiredRootPassword(t *testing.T) {
	binDir, logPath := expiredRootBin(t)
	home := t.TempDir()
	cmd := exec.Command("sh", "-c", userHomeCommand("deploy", `printf '%s|%s' "$USER" "$HOME"`))
	cmd.Env = []string{"PATH=" + binDir + ":/usr/bin:/bin", "CALL_LOG=" + logPath, "FAKE_HOME=" + home}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("userHomeCommand on an expired-root host failed: %v\n%s", err, out)
	}
	if want := "deploy|" + home; string(out) != want {
		t.Fatalf("userHomeCommand output = %q, want %q", out, want)
	}
	if calls := readCallLog(t, logPath); strings.Contains(calls, "sudo ") || !strings.Contains(calls, "runuser -u deploy --") {
		t.Fatalf("userHomeCommand must drop through runuser; calls:\n%s", calls)
	}
}

// sudoUserSwitch matches a sudo call that switches user or sets HOME, the form
// that re-enters PAM's account stage for root.
var sudoUserSwitch = regexp.MustCompile(`(^|[^[:alnum:]_-])sudo(\s+-[^\s]+)*\s+(-[A-Za-z]*[uHi][A-Za-z]*|--user(=|\s)|--set-home|--login)`)

func TestSudoUserSwitchGuardMatches(t *testing.T) {
	for _, line := range []string{`sudo -u deploy true`, `sudo -H -u "$TARGET_USER" bash`, `x && sudo -Hu deploy id`, `sudo --user=deploy id`, `sudo -n -i`} {
		if !sudoUserSwitch.MatchString(line) {
			t.Errorf("guard misses %q", line)
		}
	}
	for _, line := range []string{`runuser -u deploy -- id`, `sudo -k`, `sudo -n true`, `pseudo -u x`} {
		if sudoUserSwitch.MatchString(line) {
			t.Errorf("guard flags %q", line)
		}
	}
}

// TestBootstrapScriptNeverSwitchesUserThroughSudo keeps sudo -u / -H out of
// the bootstrap script, which runs as root: such a drop fails on hosts whose
// root password is expired (see run_as_target).
func TestBootstrapScriptNeverSwitchesUserThroughSudo(t *testing.T) {
	data, err := os.ReadFile(canonicalScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if sudoUserSwitch.MatchString(line) {
			t.Errorf("%s:%d drops privileges through sudo; use runuser (run_as_target): %s", canonicalScriptPath, i+1, strings.TrimSpace(line))
		}
	}
}

// TestHerdrCheckReportsUnverifiedInstallVersion covers the stale-install
// branch: a mise install of another Herdr version fails the digest gate with
// an actionable message, and the binary itself never runs.
func TestHerdrCheckReportsUnverifiedInstallVersion(t *testing.T) {
	for _, tc := range []struct {
		name, version, want string
	}{
		{name: "other-version", version: "0.8.0", want: "expected herdr " + HerdrVersion + ", got herdr 0.8.0 (unverified install path); rerun managed bootstrap"},
		{name: "same-version-tampered", version: HerdrVersion, want: "Herdr SHA-256 mismatch: expected "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			installDir := filepath.Join(home, ".local", "share", "mise", "installs", "herdr", tc.version)
			miseDir := filepath.Join(home, ".local", "bin")
			for _, dir := range []string{installDir, miseDir} {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
			}
			ranMarker := filepath.Join(home, "herdr-ran")
			if err := os.WriteFile(filepath.Join(installDir, "herdr"), []byte("#!/bin/sh\ntouch "+ranMarker+"\necho 'herdr "+HerdrVersion+"'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			mise := "#!/bin/sh\n[ \"$1\" = exec ] && [ \"$2\" = -- ] || exit 2\nshift 2\nPATH=" + installDir + ":$PATH exec \"$@\"\n"
			if err := os.WriteFile(filepath.Join(miseDir, "mise"), []byte(mise), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", "set -euo pipefail; "+herdrVerifiedCommand())
			cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("unverified herdr accepted:\n%s", out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Fatalf("herdr check output = %q, want %q", out, tc.want)
			}
			if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
				t.Fatal("herdr check executed an unverified binary")
			}
		})
	}
}
