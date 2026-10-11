package hostplatform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// aptFailureStatus is the status apt-get exits with on lock and repository
	// errors; the helper must hand it back unchanged.
	aptFailureStatus = 100
	// aptSuccessOutput is stdout the fake apt-get prints on success, proving
	// stdout still reaches the caller through the stderr capture.
	aptSuccessOutput = "Reading package lists..."
	// aptRepositoryError stands in for a non-lock failure that must never be
	// retried.
	aptRepositoryError = "E: The repository 'https://example.invalid noble Release' does not have a Release file."
	// alwaysLocked keeps the fake lists lock held past any budget.
	alwaysLocked = 1000
)

// fakeAPTGet fails with apt's lists-lock error for the first LOCKED_ATTEMPTS
// calls, then either fails with APT_FAILURE or succeeds. It records the call
// count and the locale apt ran under so tests can prove LC_ALL=C is forced.
const fakeAPTGet = `count=$(cat "$APT_CALLS" 2>/dev/null || printf 0)
count=$((count + 1))
printf '%s' "$count" > "$APT_CALLS"
printf '%s' "${LC_ALL:-unset}" > "$APT_LOCALE"
if [ "$count" -le "$LOCKED_ATTEMPTS" ]; then
  printf 'E: %s. It is held by process 42 (apt-get)\nE: Unable to lock directory /var/lib/apt/lists/\n' "$LOCK_ERROR" >&2
  exit "$FAILURE_STATUS"
fi
if [ -n "$APT_FAILURE" ]; then
  printf '%s\n' "$APT_FAILURE" >&2
  exit "$FAILURE_STATUS"
fi
printf '%s\n' "$SUCCESS_OUTPUT"
`

// fakeSleep records each requested interval instead of sleeping, so the full
// production budget runs in milliseconds.
const fakeSleep = `printf '%s\n' "$1" >> "$SLEEP_LOG"
`

type aptUpdateRun struct {
	status         int
	stdout, stderr string
	calls          int
	sleeps         []string
	locale         string
}

func runAPTUpdate(t *testing.T, interpreter string, lockedAttempts int, failure string) aptUpdateRun {
	t.Helper()
	if _, err := exec.LookPath(interpreter); err != nil {
		t.Skipf("%s is not available", interpreter)
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"apt-get": fakeAPTGet, "sleep": fakeSleep} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	scriptPath := filepath.Join(dir, "update.sh")
	if err := os.WriteFile(scriptPath, []byte("set -eu\n"+APTUpdateShellFunction()+"apt_update\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	callsPath := filepath.Join(dir, "calls")
	localePath := filepath.Join(dir, "locale")
	sleepLog := filepath.Join(dir, "sleeps")
	cmd := exec.Command(interpreter, scriptPath)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"LC_ALL=tr_TR.UTF-8",
		"APT_CALLS="+callsPath,
		"APT_LOCALE="+localePath,
		"SLEEP_LOG="+sleepLog,
		"LOCKED_ATTEMPTS="+strconv.Itoa(lockedAttempts),
		"APT_FAILURE="+failure,
		"LOCK_ERROR="+APTListsLockError,
		"FAILURE_STATUS="+strconv.Itoa(aptFailureStatus),
		"SUCCESS_OUTPUT="+aptSuccessOutput,
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	run := aptUpdateRun{}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run apt_update: %v", err)
		}
		run.status = exitErr.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	if run.calls, err = strconv.Atoi(string(calls)); err != nil {
		t.Fatal(err)
	}
	locale, err := os.ReadFile(localePath)
	if err != nil {
		t.Fatal(err)
	}
	run.locale = string(locale)
	if sleeps, err := os.ReadFile(sleepLog); err == nil {
		run.sleeps = strings.Fields(string(sleeps))
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return run
}

// aptUpdateInterpreters covers dash, which runs the rendered scripts on
// Ubuntu, and bash, which runs them in the remote test harnesses.
var aptUpdateInterpreters = []string{"dash", "bash"}

func TestAPTUpdateShellFunctionWaitsForListsLockThenSucceeds(t *testing.T) {
	for _, interpreter := range aptUpdateInterpreters {
		t.Run(interpreter, func(t *testing.T) {
			run := runAPTUpdate(t, interpreter, 2, "")
			if run.status != 0 {
				t.Fatalf("status = %d, want 0\nstderr: %s", run.status, run.stderr)
			}
			if run.calls != 3 || len(run.sleeps) != 2 {
				t.Fatalf("calls = %d, sleeps = %v; want 3 calls and 2 sleeps", run.calls, run.sleeps)
			}
			for _, interval := range run.sleeps {
				if interval != strconv.Itoa(APTListsLockRetrySeconds) {
					t.Fatalf("sleep interval = %s, want %d", interval, APTListsLockRetrySeconds)
				}
			}
			if run.locale != "C" {
				t.Fatalf("apt-get ran under LC_ALL=%q, want C so the lock message matches", run.locale)
			}
			if !strings.Contains(run.stdout, aptSuccessOutput) {
				t.Fatalf("apt stdout was not passed through: %q", run.stdout)
			}
			if !strings.Contains(run.stderr, "waiting up to "+strconv.Itoa(APTLockTimeoutSeconds)+" seconds") || strings.Count(run.stderr, "waiting up to") != 1 {
				t.Fatalf("want one wait notice on stderr, got %q", run.stderr)
			}
		})
	}
}

func TestAPTUpdateShellFunctionGivesUpAfterBudget(t *testing.T) {
	for _, interpreter := range aptUpdateInterpreters {
		t.Run(interpreter, func(t *testing.T) {
			run := runAPTUpdate(t, interpreter, alwaysLocked, "")
			if run.status != aptFailureStatus {
				t.Fatalf("status = %d, want apt's %d", run.status, aptFailureStatus)
			}
			wantSleeps := APTLockTimeoutSeconds / APTListsLockRetrySeconds
			if len(run.sleeps) != wantSleeps || run.calls != wantSleeps+1 {
				t.Fatalf("calls = %d, sleeps = %d; want %d calls and %d sleeps", run.calls, len(run.sleeps), wantSleeps+1, wantSleeps)
			}
			for _, want := range []string{
				APTListsLockError,
				"apt package lists stayed locked for " + strconv.Itoa(APTLockTimeoutSeconds) + " seconds",
			} {
				if !strings.Contains(run.stderr, want) {
					t.Fatalf("stderr missing %q: %q", want, run.stderr)
				}
			}
		})
	}
}

func TestAPTUpdateShellFunctionFailsFastOnOtherErrors(t *testing.T) {
	for _, interpreter := range aptUpdateInterpreters {
		t.Run(interpreter, func(t *testing.T) {
			run := runAPTUpdate(t, interpreter, 0, aptRepositoryError)
			if run.status != aptFailureStatus {
				t.Fatalf("status = %d, want apt's %d", run.status, aptFailureStatus)
			}
			if run.calls != 1 || len(run.sleeps) != 0 {
				t.Fatalf("calls = %d, sleeps = %v; a non-lock failure must not retry", run.calls, run.sleeps)
			}
			if !strings.Contains(run.stderr, aptRepositoryError) || strings.Contains(run.stderr, "waiting up to") {
				t.Fatalf("want only apt's own error on stderr, got %q", run.stderr)
			}
		})
	}
}

func TestAPTLockTimeoutOptionUsesSharedBudget(t *testing.T) {
	if got, want := APTLockTimeoutOption(), "DPkg::Lock::Timeout="+strconv.Itoa(APTLockTimeoutSeconds); got != want {
		t.Fatalf("APTLockTimeoutOption() = %q, want %q", got, want)
	}
	if APTLockTimeout.Seconds() != APTLockTimeoutSeconds {
		t.Fatalf("APTLockTimeout = %v, want %d seconds", APTLockTimeout, APTLockTimeoutSeconds)
	}
}
