package tailscaletools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/hostplatform"
)

const (
	// testAPTFailureStatus is apt-get's status for lock and repository
	// errors; the helper must return it unchanged.
	testAPTFailureStatus = 100
	// testAPTRepositoryError stands in for a failure that must never retry.
	testAPTRepositoryError = "E: The repository 'https://example.invalid noble Release' does not have a Release file."
	// testAPTAlwaysLocked keeps the fake lists lock held past any budget.
	testAPTAlwaysLocked = 1000
)

// testFakeAPTGet fails with apt's lists-lock error for the first
// LOCKED_ATTEMPTS calls, then fails with APT_FAILURE or succeeds, counting
// calls so tests can prove how often the updater retried.
const testFakeAPTGet = `count=$(cat "$APT_CALLS" 2>/dev/null || printf 0)
count=$((count + 1))
printf '%s' "$count" > "$APT_CALLS"
if [ "$count" -le "$LOCKED_ATTEMPTS" ]; then
  printf 'E: %s. It is held by process 42 (apt-get)\n' "$LOCK_ERROR" >&2
  exit "$FAILURE_STATUS"
fi
if [ -n "$APT_FAILURE" ]; then
  printf '%s\n' "$APT_FAILURE" >&2
  exit "$FAILURE_STATUS"
fi
printf 'Reading package lists...\n'
`

type tailscaleAPTUpdateRun struct {
	code           int
	stdout, stderr string
	calls          int
	sleeps         []string
}

// runTailscaleAPTUpdate sources the rendered updater so apt_update runs with
// the exact values UpdateScript ships; a fake sleep keeps the full budget fast.
func runTailscaleAPTUpdate(t *testing.T, lockedAttempts int, failure string) tailscaleAPTUpdateRun {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "update.sh")
	if err := os.WriteFile(scriptPath, []byte(UpdateScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTailscaleFakeCommand(t, binDir, "apt-get", testFakeAPTGet)
	writeTailscaleFakeCommand(t, binDir, "sleep", "printf '%s\\n' \"$1\" >> \"$SLEEP_LOG\"\n")
	callsPath := filepath.Join(dir, "calls")
	sleepLog := filepath.Join(dir, "sleeps")
	cmd := exec.Command("bash", "-c", `source "$1"; apt_update`, "test", scriptPath)
	cmd.Env = append(os.Environ(),
		"SERVERPRO_TAILSCALE_SOURCE_ONLY=1",
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"APT_CALLS="+callsPath,
		"SLEEP_LOG="+sleepLog,
		"LOCKED_ATTEMPTS="+strconv.Itoa(lockedAttempts),
		"APT_FAILURE="+failure,
		"LOCK_ERROR="+hostplatform.APTListsLockError,
		"FAILURE_STATUS="+strconv.Itoa(testAPTFailureStatus),
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	run := tailscaleAPTUpdateRun{}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run apt_update: %v", err)
		}
		run.code = exitErr.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	if run.calls, err = strconv.Atoi(string(calls)); err != nil {
		t.Fatal(err)
	}
	if sleeps, err := os.ReadFile(sleepLog); err == nil {
		run.sleeps = strings.Fields(string(sleeps))
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return run
}

func TestUpdateScriptAPTUpdateWaitsForListsLockThenSucceeds(t *testing.T) {
	run := runTailscaleAPTUpdate(t, 2, "")
	if run.code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", run.code, run.stderr)
	}
	if run.calls != 3 || len(run.sleeps) != 2 || run.sleeps[0] != strconv.Itoa(hostplatform.APTListsLockRetrySeconds) {
		t.Fatalf("calls = %d, sleeps = %v; want 3 calls and 2 waits of %d seconds", run.calls, run.sleeps, hostplatform.APTListsLockRetrySeconds)
	}
	if !strings.Contains(run.stdout, "Reading package lists...") {
		t.Fatalf("apt stdout was not passed through: %q", run.stdout)
	}
	if strings.Count(run.stderr, "waiting up to "+strconv.Itoa(hostplatform.APTLockTimeoutSeconds)+" seconds") != 1 {
		t.Fatalf("want one wait notice on stderr, got %q", run.stderr)
	}
}

func TestUpdateScriptAPTUpdateGivesUpAfterBudget(t *testing.T) {
	run := runTailscaleAPTUpdate(t, testAPTAlwaysLocked, "")
	if run.code != testAPTFailureStatus {
		t.Fatalf("exit = %d, want apt's %d", run.code, testAPTFailureStatus)
	}
	wantSleeps := hostplatform.APTLockTimeoutSeconds / hostplatform.APTListsLockRetrySeconds
	if len(run.sleeps) != wantSleeps || run.calls != wantSleeps+1 {
		t.Fatalf("calls = %d, sleeps = %d; want %d calls and %d sleeps", run.calls, len(run.sleeps), wantSleeps+1, wantSleeps)
	}
	if want := "apt package lists stayed locked for " + strconv.Itoa(hostplatform.APTLockTimeoutSeconds) + " seconds"; !strings.Contains(run.stderr, want) || !strings.Contains(run.stderr, hostplatform.APTListsLockError) {
		t.Fatalf("stderr must carry apt's lock error and %q, got %q", want, run.stderr)
	}
}

func TestUpdateScriptAPTUpdateFailsFastOnOtherErrors(t *testing.T) {
	run := runTailscaleAPTUpdate(t, 0, testAPTRepositoryError)
	if run.code != testAPTFailureStatus {
		t.Fatalf("exit = %d, want apt's %d", run.code, testAPTFailureStatus)
	}
	if run.calls != 1 || len(run.sleeps) != 0 {
		t.Fatalf("calls = %d, sleeps = %v; a non-lock failure must not retry", run.calls, run.sleeps)
	}
	if !strings.Contains(run.stderr, testAPTRepositoryError) || strings.Contains(run.stderr, "waiting up to") {
		t.Fatalf("want only apt's own error on stderr, got %q", run.stderr)
	}
}

// TestUpdateScriptShipsSharedAPTWaitValues keeps the updater on the
// host-wide budget instead of a copied literal.
func TestUpdateScriptShipsSharedAPTWaitValues(t *testing.T) {
	script := UpdateScript()
	for _, want := range []string{
		"SERVERPRO_TAILSCALE_APT_LOCK_TIMEOUT='" + strconv.Itoa(hostplatform.APTLockTimeoutSeconds) + "'",
		"SERVERPRO_TAILSCALE_APT_LISTS_LOCK_RETRY_SECONDS='" + strconv.Itoa(hostplatform.APTListsLockRetrySeconds) + "'",
		"SERVERPRO_TAILSCALE_APT_LISTS_LOCK_ERROR='" + hostplatform.APTListsLockError + "'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("update script missing %q", want)
		}
	}
	if strings.Contains(script, "Lock::Timeout=300") {
		t.Fatal("update script still carries a literal lock timeout")
	}
}
