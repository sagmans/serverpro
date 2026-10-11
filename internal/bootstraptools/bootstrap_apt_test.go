package bootstraptools

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/hostplatform"
)

const (
	// testAPTFailureStatus is apt-get's status for lock and repository
	// errors; the helper must return it unchanged.
	testAPTFailureStatus = "100"
	// testAPTRepositoryError stands in for a failure that must never retry.
	testAPTRepositoryError = "E: The repository 'https://example.invalid noble Release' does not have a Release file."
	// testAPTAlwaysLocked keeps the fake lists lock held past any budget.
	testAPTAlwaysLocked = "1000"
)

// testFakeAPTGet fails with apt's lists-lock error for the first
// LOCKED_ATTEMPTS calls, then fails with APT_FAILURE or succeeds, counting
// calls so tests can prove how often the helper retried.
const testFakeAPTGet = `#!/bin/sh
count=$(cat "$APT_CALLS" 2>/dev/null || printf 0)
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

// testFakeSleep records intervals instead of sleeping, so the production
// budget runs in milliseconds.
const testFakeSleep = `#!/bin/sh
printf '%s\n' "$1" >> "$SLEEP_LOG"
`

type bootstrapAPTUpdateRun struct {
	code           int
	stdout, stderr string
	calls          string
	sleeps         []string
}

func runBootstrapAPTUpdate(t *testing.T, lockedAttempts, failure string) bootstrapAPTUpdateRun {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"apt-get": testFakeAPTGet, "sleep": testFakeSleep} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	callsPath := filepath.Join(dir, "calls")
	sleepLog := filepath.Join(dir, "sleeps")
	code, stdout, stderr := runHelper(t, `apt_update`, append(manifestEnv(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"APT_CALLS="+callsPath,
		"SLEEP_LOG="+sleepLog,
		"LOCKED_ATTEMPTS="+lockedAttempts,
		"APT_FAILURE="+failure,
		"LOCK_ERROR="+hostplatform.APTListsLockError,
		"FAILURE_STATUS="+testAPTFailureStatus,
	)...)
	run := bootstrapAPTUpdateRun{code: code, stdout: stdout, stderr: stderr}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	run.calls = string(calls)
	if sleeps, err := os.ReadFile(sleepLog); err == nil {
		run.sleeps = strings.Fields(string(sleeps))
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return run
}

func TestShellAPTUpdateWaitsForListsLockThenSucceeds(t *testing.T) {
	run := runBootstrapAPTUpdate(t, "2", "")
	if run.code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", run.code, run.stderr)
	}
	if run.calls != "3" || len(run.sleeps) != 2 || run.sleeps[0] != strconv.Itoa(hostplatform.APTListsLockRetrySeconds) {
		t.Fatalf("calls = %s, sleeps = %v; want 3 calls and 2 waits of %d seconds", run.calls, run.sleeps, hostplatform.APTListsLockRetrySeconds)
	}
	if !strings.Contains(run.stdout, "Reading package lists...") {
		t.Fatalf("apt stdout was not passed through: %q", run.stdout)
	}
	if strings.Count(run.stderr, "waiting up to "+strconv.Itoa(hostplatform.APTLockTimeoutSeconds)+" seconds") != 1 {
		t.Fatalf("want one wait notice on stderr, got %q", run.stderr)
	}
}

func TestShellAPTUpdateGivesUpAfterBudget(t *testing.T) {
	run := runBootstrapAPTUpdate(t, testAPTAlwaysLocked, "")
	if strconv.Itoa(run.code) != testAPTFailureStatus {
		t.Fatalf("exit = %d, want apt's %s", run.code, testAPTFailureStatus)
	}
	wantSleeps := hostplatform.APTLockTimeoutSeconds / hostplatform.APTListsLockRetrySeconds
	if len(run.sleeps) != wantSleeps || run.calls != strconv.Itoa(wantSleeps+1) {
		t.Fatalf("calls = %s, sleeps = %d; want %d calls and %d sleeps", run.calls, len(run.sleeps), wantSleeps+1, wantSleeps)
	}
	if want := "apt package lists stayed locked for " + strconv.Itoa(hostplatform.APTLockTimeoutSeconds) + " seconds"; !strings.Contains(run.stderr, want) || !strings.Contains(run.stderr, hostplatform.APTListsLockError) {
		t.Fatalf("stderr must carry apt's lock error and %q, got %q", want, run.stderr)
	}
}

func TestShellAPTUpdateFailsFastOnOtherErrors(t *testing.T) {
	run := runBootstrapAPTUpdate(t, "0", testAPTRepositoryError)
	if strconv.Itoa(run.code) != testAPTFailureStatus {
		t.Fatalf("exit = %d, want apt's %s", run.code, testAPTFailureStatus)
	}
	if run.calls != "1" || len(run.sleeps) != 0 {
		t.Fatalf("calls = %s, sleeps = %v; a non-lock failure must not retry", run.calls, run.sleeps)
	}
	if !strings.Contains(run.stderr, testAPTRepositoryError) || strings.Contains(run.stderr, "waiting up to") {
		t.Fatalf("want only apt's own error on stderr, got %q", run.stderr)
	}
}

// TestShellBootstrapSecondsEnvRejectsUnsafeWaits keeps the wait settings to
// positive decimal seconds: zero would retry without spending the budget.
func TestShellBootstrapSecondsEnvRejectsUnsafeWaits(t *testing.T) {
	assertShellValidator(t, `SERVERPRO_TEST_SECONDS="$TESTVAL" bootstrap_seconds_env SERVERPRO_TEST_SECONDS`, []validatorCase{
		{"budget", "300", false},
		{"interval", "5", false},
		{"zero", "0", true},
		{"leading-zero", "05", true},
		{"negative", "-5", true},
		{"fraction", "1.5", true},
		{"empty", "", true},
	})
}

func TestManagedPackageRefreshCommand(t *testing.T) {
	if got, want := ManagedPackageRefreshCommand(), hostplatform.APTUpdateShellFunction()+"apt_update"; got != want {
		t.Fatalf("ManagedPackageRefreshCommand() = %q, want the shared apt_update helper and its call", got)
	}
}

// TestBootstrapScriptRoutesDpkgLockWaitsThroughAPTGet keeps the dpkg-lock
// budget in one helper fed by the manifest, so no call site can drift back to
// a literal timeout or skip the wait.
func TestBootstrapScriptRoutesDpkgLockWaitsThroughAPTGet(t *testing.T) {
	if got := strings.Count(installScript, "-o DPkg::Lock::Timeout"); got != 1 {
		t.Fatalf("DPkg::Lock::Timeout appears %d times, want only inside apt_get", got)
	}
	if !strings.Contains(installScript, `apt-get -o DPkg::Lock::Timeout="${timeout}" "$@"`) {
		t.Fatal("apt_get must pass the manifest lock timeout")
	}
}
