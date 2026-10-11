package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/tunnel"
)

// staleBatchRemote serves a batch baseline, then answers live reads from a
// per-script queue, so tests can model a host that changed after the baseline.
type staleBatchRemote struct {
	baselineFail map[string]string
	live         map[string][]remoteCall
	runScripts   []string
	deadlines    map[string]time.Duration
}

func (r *staleBatchRemote) RunBatch(_ context.Context, _, _ string, commands []remote.BatchCommand) ([]remote.BatchResult, error) {
	results := make([]remote.BatchResult, len(commands))
	for i, command := range commands {
		results[i].Output = "ok"
		if out, ok := r.baselineFail[command.Script]; ok {
			results[i].Output = out
			results[i].Err = errBaselineFail
		}
	}
	return results, nil
}

var errBaselineFail = &remoteTestError{"baseline read failed"}

type remoteTestError struct{ msg string }

func (e *remoteTestError) Error() string { return e.msg }

func (r *staleBatchRemote) Run(ctx context.Context, _, _, script string) (string, error) {
	r.runScripts = append(r.runScripts, script)
	if deadline, ok := ctx.Deadline(); ok {
		if r.deadlines == nil {
			r.deadlines = map[string]time.Duration{}
		}
		r.deadlines[script] = time.Until(deadline)
	}
	if calls := r.live[script]; len(calls) > 0 {
		r.live[script] = calls[1:]
		return calls[0].out, calls[0].err
	}
	return "ok", nil
}

func countCommand(commands []string, script string) int {
	n := 0
	for _, command := range commands {
		if command == script {
			n++
		}
	}
	return n
}

func resultByName(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, result := range results {
		if result.Name == name {
			return result
		}
	}
	t.Fatalf("no %q result in %+v", name, results)
	return Result{}
}

// TestRemoteChecksReReadSSHDUnitsAfterSSHDRepair is the stale-baseline case:
// the baseline saw healthy units, the sshd repair then left ssh.service
// failed, and the units check must see that live and repair it.
func TestRemoteChecksReReadSSHDUnitsAfterSSHDRepair(t *testing.T) {
	cfg := config.Example("prod")
	agent := sshdSettingValueCommand(sshdKeywordAllowAgentForwarding, sshdValueDisabled)
	tcp := sshdSettingValueCommand(sshdKeywordAllowTCPForwarding, sshdValueDisabled)
	runner := &staleBatchRemote{
		baselineFail: map[string]string{agent: "allowagentforwarding yes", tcp: "allowtcpforwarding yes"},
		live: map[string][]remoteCall{
			sshdUnitsReadCommand(): {{out: "unhealthy ssh units: ssh.service:failed", err: errBaselineFail}, {out: "ssh units healthy"}},
		},
	}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{Fix: true})

	if got := countCommand(runner.runScripts, sshdSettingsFixCommand()); got != 1 {
		t.Fatalf("sshd repair ran %d times, want once: later sshd checks must re-read instead of repairing again", got)
	}
	if res := resultByName(t, results, "sshd agent forwarding"); res.Status != Pass || !strings.Contains(res.Evidence, "fixed") {
		t.Fatalf("agent forwarding = %+v", res)
	}
	// The tcp baseline failed too, but the shared repair already fixed it.
	if res := resultByName(t, results, "sshd tcp forwarding"); res.Status != Pass || strings.Contains(res.Evidence, "fixed") {
		t.Fatalf("tcp forwarding = %+v, want a live pass without a second repair", res)
	}
	if res := resultByName(t, results, sshdUnitsCheckName); res.Status != Pass || !strings.Contains(res.Evidence, "fixed") {
		t.Fatalf("sshd units = %+v, want the live failure repaired", res)
	}
	if countCommand(runner.runScripts, sshdUnitsFixCommand()) != 1 || countCommand(runner.runScripts, "ss -H -tuln") != 1 {
		t.Fatalf("units fix or listening-ports live read missing: %q", runner.runScripts)
	}
}

func TestRemoteChecksKeepSSHDBaselineWithoutSSHDRepair(t *testing.T) {
	cfg := config.Example("prod")
	runner := &staleBatchRemote{live: map[string][]remoteCall{
		sshdUnitsReadCommand(): {{out: "unhealthy ssh units: ssh.service:failed", err: errBaselineFail}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{Fix: true})
	if res := resultByName(t, results, sshdUnitsCheckName); res.Status != Pass {
		t.Fatalf("sshd units = %+v, want the baseline when no sshd repair ran", res)
	}
	if countCommand(runner.runScripts, sshdUnitsReadCommand()) != 0 {
		t.Fatalf("units re-read without an sshd repair: %q", runner.runScripts)
	}
}

// TestSSHDCommandsNeverUseSudo keeps nested sudo out of every sshd check and
// repair: they run in the runner's root shell, and root-to-root sudo fails on
// hosts whose root password is expired.
func TestSSHDCommandsNeverUseSudo(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "cloudflare-tunnel"
	sudo := regexp.MustCompile(`(^|[^[:alnum:]_-])sudo([^[:alnum:]_-]|$)`)
	checked := 0
	for _, specification := range remoteCheckSpecifications(cfg) {
		for _, command := range append(append([]string(nil), specification.readCommands...), specification.liveCommands...) {
			if !strings.Contains(command, "sshd ") && !strings.Contains(command, sshSocketUnit) && !strings.Contains(command, sshServiceUnit) {
				continue
			}
			checked++
			if sudo.MatchString(command) {
				t.Errorf("sshd command uses sudo:\n%s", command)
			}
		}
	}
	if checked < 12 {
		t.Fatalf("guard only saw %d sshd commands", checked)
	}
}

// TestSSHDSettingCheckSurvivesExpiredRootPassword runs a real sshd read
// command against a fake sshd and a sudo that fails as root.
func TestSSHDSettingCheckSurvivesExpiredRootPassword(t *testing.T) {
	bin := t.TempDir()
	writeExecutable(t, bin, "sudo", "echo 'sudo: Account or password is expired, reset your password and try again' >&2\nexit 1")
	writeExecutable(t, bin, "sshd", "[ \"$1\" = -T ] || exit 2\nprintf 'permitrootlogin no\\nallowagentforwarding no\\n'")
	cmd := exec.Command("sh", "-c", sshdSettingValueCommand(sshdKeywordAllowAgentForwarding, sshdValueDisabled))
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sshd check failed on an expired-root host: %v\n%s", err, out)
	}
}

func writeExecutable(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeSystemctl models the unit states the ssh repair has to handle. Unit
// state lives in $STATE/<unit>.active (active, inactive, failed) and
// $STATE/<unit>.enabled; $STATE/<unit>.startlimit makes start fail with
// start-limit-hit until reset-failed clears it, as systemd does.
const fakeSystemctl = `cmd=$1; shift
[ "${1:-}" = --quiet ] && shift
unit=$1
case "$unit" in *.*) ;; *) unit="$unit.service" ;; esac
printf '%s %s\n' "$cmd" "$unit" >>"$STATE/log"
state() { cat "$STATE/$unit.active" 2>/dev/null || echo inactive; }
case "$cmd" in
  is-failed) [ "$(state)" = failed ] ;;
  is-active) [ "$(state)" = active ] ;;
  is-enabled) [ "$(cat "$STATE/$unit.enabled" 2>/dev/null)" = yes ] ;;
  reset-failed) rm -f "$STATE/$unit.startlimit"; [ "$(state)" != failed ] || echo inactive >"$STATE/$unit.active" ;;
  start)
    if [ -e "$STATE/$unit.startlimit" ]; then
      echo "Job for $unit failed. start-limit-hit" >&2
      echo failed >"$STATE/$unit.active"
      exit 1
    fi
    echo active >"$STATE/$unit.active" ;;
  try-reload-or-restart) [ "$(state)" != active ] || echo "reload $unit" >>"$STATE/reloads" ;;
  *) echo "unexpected systemctl $cmd" >&2; exit 2 ;;
esac`

type unitState struct {
	active, enabled string
	startLimit      bool
}

func runWithFakeSystemctl(t *testing.T, units map[string]unitState, scripts ...string) (string, []error, string) {
	t.Helper()
	bin, state := t.TempDir(), t.TempDir()
	writeExecutable(t, bin, "systemctl", fakeSystemctl)
	for unit, s := range units {
		for file, value := range map[string]string{".active": s.active, ".enabled": s.enabled} {
			if err := os.WriteFile(filepath.Join(state, unit+file), []byte(value+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if s.startLimit {
			if err := os.WriteFile(filepath.Join(state, unit+".startlimit"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var outputs strings.Builder
	var errs []error
	for _, script := range scripts {
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "STATE=" + state}
		out, err := cmd.CombinedOutput()
		outputs.Write(out)
		errs = append(errs, err)
	}
	log, _ := os.ReadFile(filepath.Join(state, "log"))
	return outputs.String(), errs, string(log)
}

func TestSSHDUnitsReadAndFixAgainstFakeSystemctl(t *testing.T) {
	for _, tc := range []struct {
		name         string
		units        map[string]unitState
		readEvidence string // empty: the first read passes
		wantStart    string
	}{
		{
			name:         "service-failed",
			units:        map[string]unitState{sshServiceUnit: {active: "failed", enabled: "yes"}, sshSocketUnit: {active: "inactive", enabled: "no"}},
			readEvidence: "ssh.service:failed",
			wantStart:    "start ssh.service",
		},
		{
			name:         "service-enabled-but-inactive",
			units:        map[string]unitState{sshServiceUnit: {active: "inactive", enabled: "yes"}, sshSocketUnit: {active: "inactive", enabled: "no"}},
			readEvidence: "ssh.service:inactive",
			wantStart:    "start ssh.service",
		},
		{
			name:  "socket-active-service-inactive",
			units: map[string]unitState{sshServiceUnit: {active: "inactive", enabled: "yes"}, sshSocketUnit: {active: "active", enabled: "yes"}},
		},
		{
			name:         "start-limit-hit",
			units:        map[string]unitState{sshServiceUnit: {active: "failed", enabled: "yes", startLimit: true}, sshSocketUnit: {active: "inactive", enabled: "no"}},
			readEvidence: "ssh.service:failed",
			wantStart:    "start ssh.service",
		},
		{
			name:         "socket-failed-prefers-socket",
			units:        map[string]unitState{sshServiceUnit: {active: "inactive", enabled: "yes"}, sshSocketUnit: {active: "failed", enabled: "yes"}},
			readEvidence: "ssh.socket:failed",
			wantStart:    "start ssh.socket",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errs, log := runWithFakeSystemctl(t, tc.units, sshdUnitsReadCommand(), sshdUnitsFixCommand(), sshdUnitsReadCommand())
			if tc.readEvidence == "" {
				if errs[0] != nil || !strings.Contains(out, "ssh units healthy") {
					t.Fatalf("healthy units reported unhealthy: %v\n%s", errs[0], out)
				}
			} else if errs[0] == nil || !strings.Contains(out, tc.readEvidence) {
				t.Fatalf("read = %v %q, want failure naming %q", errs[0], out, tc.readEvidence)
			}
			if errs[1] != nil {
				t.Fatalf("units fix failed: %v\n%s\n%s", errs[1], out, log)
			}
			if errs[2] != nil {
				t.Fatalf("units still unhealthy after fix: %v\n%s\n%s", errs[2], out, log)
			}
			if strings.Contains(log, "enable ") {
				t.Fatalf("units fix must never enable a unit:\n%s", log)
			}
			if tc.wantStart != "" && !strings.Contains(log, tc.wantStart) {
				t.Fatalf("fix log missing %q:\n%s", tc.wantStart, log)
			}
			if tc.wantStart == "" && strings.Contains(log, "start ") {
				t.Fatalf("healthy units must not be started:\n%s", log)
			}
		})
	}
}

// sshdFixTree is a scratch /etc/ssh for running sshdSettingsFixScript.
type sshdFixTree struct {
	root, sshDir, bin, state string
}

func newSSHDFixTree(t *testing.T, main, dropin string, active bool) sshdFixTree {
	t.Helper()
	root := t.TempDir()
	tree := sshdFixTree{root: root, sshDir: filepath.Join(root, "etc", "ssh"), bin: filepath.Join(root, "bin"), state: filepath.Join(root, "state")}
	for _, dir := range []string{filepath.Join(tree.sshDir, "sshd_config.d"), tree.bin, tree.state} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(tree.sshDir, "sshd_config"), main)
	if dropin != "" {
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config.d", "99-serverpro.conf"), dropin)
	}
	serviceState := "inactive"
	if active {
		serviceState = "active"
	}
	writeFile(t, filepath.Join(tree.state, sshServiceUnit+".active"), serviceState)
	writeExecutable(t, tree.bin, "systemctl", fakeSystemctl)
	// FAIL_LIVE_VALIDATION=1 fails the first live sshd -t only, as a drop-in
	// changing under the repair would; =always keeps failing after a restore.
	// The fake sshd -t reads the main config (-f or the live one) plus its
	// Include globs, logs what it read, and rejects any BADCONF line. sshd -T
	// prints the first value of each keyword, follows Includes (relative ones
	// from the config directory) and stops at the first Match block, as sshd
	// does for the global settings.
	writeExecutable(t, tree.bin, "sshd", fakeSSHD)
	// Some mktemp builds ignore TMPDIR without a template; pinning the scratch
	// root keeps the leftover checks honest on every platform.
	writeExecutable(t, tree.bin, "mktemp", `exec /usr/bin/mktemp -d "$TMPDIR/tmp.XXXXXXXXXX"`)
	writeExecutable(t, tree.bin, "mv", `printf 'mv %s\n' "$*" >>"$STATE/mv.log"
for arg; do case "$arg" in ${FAIL_MV_MATCH:-/nonexistent/}) echo "mv: simulated failure for $arg" >&2; exit 1 ;; esac; done
exec /bin/mv "$@"`)
	return tree
}

const fakeSSHD = `case "$1" in
-T)
  set -f
  seen=' '
  read_cfg() {
    while IFS= read -r line || [ -n "$line" ]; do
      set -- $line
      [ $# -ge 2 ] || continue
      key=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
      case "$key" in
        match) return 1 ;;
        include)
          shift
          for pat; do
            case "$pat" in /*) ;; *) pat="$(dirname "$LIVE_MAIN")/$pat" ;; esac
            set +f
            for f in $pat; do
              if [ -e "$f" ]; then read_cfg "$f" || return 1; fi
            done
            set -f
          done
          continue ;;
        challengeresponseauthentication) key=kbdinteractiveauthentication ;;
      esac
      case "$seen" in *" $key "*) continue ;; esac
      seen="$seen$key "
      printf '%s %s\n' "$key" "$2"
    done <"$1"
  }
  read_cfg "$LIVE_MAIN"
  exit 0 ;;
-t) ;;
*) exit 2 ;;
esac
cfg="$LIVE_MAIN"
if [ "${2:-}" = -f ]; then cfg=$3; [ -z "${SIGNAL_ON_CANDIDATE:-}" ] || kill -s "$SIGNAL_ON_CANDIDATE" "$PPID"
elif [ "${FAIL_LIVE_VALIDATION:-}" = always ] || { [ -n "${FAIL_LIVE_VALIDATION:-}" ] && [ ! -e "$STATE/live-failed" ]; }; then
  : >"$STATE/live-failed"; echo 'live validation failed' >&2; exit 1
fi
printf 'sshd -t %s\n' "$cfg" >>"$STATE/sshd.log"
files="$cfg $(awk 'tolower($1) == "include" { print $2 }' "$cfg")"
for f in $files; do
  case "$f" in /*) ;; *) f="$(dirname "$LIVE_MAIN")/$f" ;; esac
  for g in $f; do
    [ -e "$g" ] || continue
    printf 'read %s\n' "$g" >>"$STATE/sshd.log"
    if grep -q BADCONF "$g"; then echo "$g: bad configuration" >&2; exit 1; fi
  done
done`

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (tree sshdFixTree) run(t *testing.T, extraEnv ...string) (string, error) {
	t.Helper()
	script := sshdSettingsFixScript(tree.sshDir, filepath.Join(tree.root, "run", "sshd"))
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append([]string{
		"PATH=" + tree.bin + ":/usr/bin:/bin", "STATE=" + tree.state,
		"LIVE_MAIN=" + filepath.Join(tree.sshDir, "sshd_config"), "TMPDIR=" + tree.root,
	}, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (tree sshdFixTree) read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tree.sshDir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return "<absent>"
		}
		t.Fatal(err)
	}
	return string(data)
}

func (tree sshdFixTree) stateFile(name string) string {
	data, _ := os.ReadFile(filepath.Join(tree.state, name))
	return string(data)
}

func managedDropin(t *testing.T) string {
	t.Helper()
	return sshdManagedDropin()
}

const includedMain = "Include /ETC/sshd_config.d/*.conf\nPort 22\n"

func (tree sshdFixTree) includeMain() string {
	return strings.ReplaceAll(includedMain, "/ETC", tree.sshDir)
}

func TestSSHDSettingsFixIdempotenceScript(t *testing.T) {
	t.Run("no-change-service-inactive", func(t *testing.T) {
		tree := newSSHDFixTree(t, "", managedDropin(t), false)
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config"), tree.includeMain())
		before := tree.read(t, "sshd_config")
		if out, err := tree.run(t); err != nil {
			t.Fatalf("fix failed: %v\n%s", err, out)
		}
		if tree.read(t, "sshd_config") != before || tree.read(t, "sshd_config.d/99-serverpro.conf") != managedDropin(t) {
			t.Fatal("unchanged config was rewritten")
		}
		if reloads := tree.stateFile("reloads"); reloads != "" {
			t.Fatalf("inactive ssh.service reloaded: %q", reloads)
		}
		if !strings.Contains(tree.stateFile("sshd.log"), "sshd -t "+filepath.Join(tree.sshDir, "sshd_config")) {
			t.Fatalf("unchanged config not validated: %q", tree.stateFile("sshd.log"))
		}
	})
	t.Run("no-change-service-active", func(t *testing.T) {
		tree := newSSHDFixTree(t, "", managedDropin(t), true)
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config"), tree.includeMain())
		if out, err := tree.run(t); err != nil {
			t.Fatalf("fix failed: %v\n%s", err, out)
		}
		if tree.read(t, "sshd_config.d/99-serverpro.conf") != managedDropin(t) {
			t.Fatal("unchanged drop-in was rewritten")
		}
		// A running daemon is reloaded once even when the files match, in
		// case an earlier reload failed after its write.
		if reloads := tree.stateFile("reloads"); reloads != "reload ssh.service\n" {
			t.Fatalf("reloads = %q, want exactly one", reloads)
		}
		if strings.Contains(tree.stateFile("sshd.log"), "sshd -t "+tree.root+"/tmp.") {
			t.Fatal("unchanged config ran the candidate validation")
		}
	})
	t.Run("changed-adds-include-and-dropin", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "PermitRootLogin yes\n", true)
		if out, err := tree.run(t); err != nil {
			t.Fatalf("fix failed: %v\n%s", err, out)
		}
		if got, want := tree.read(t, "sshd_config"), "Include "+tree.sshDir+"/sshd_config.d/*.conf\nPort 22\n"; got != want {
			t.Fatalf("main config = %q, want %q", got, want)
		}
		if tree.read(t, "sshd_config.d/99-serverpro.conf") != managedDropin(t) {
			t.Fatal("drop-in not written")
		}
		log := tree.stateFile("sshd.log")
		// The candidate is validated from the private copy before the live write.
		candidate := strings.Index(log, "sshd -t "+tree.root+"/tmp.")
		live := strings.Index(log, "sshd -t "+filepath.Join(tree.sshDir, "sshd_config"))
		if candidate < 0 || live < 0 || candidate > live || !regexp.MustCompile(`read \S+/d/99-serverpro\.conf`).MatchString(log) {
			t.Fatalf("candidate must be validated against its own drop-in copy before the live check:\n%s", log)
		}
		if tree.stateFile("reloads") != "reload ssh.service\n" {
			t.Fatalf("changed config not reloaded once: %q", tree.stateFile("reloads"))
		}
		assertNoLeftovers(t, tree)
		// Running again is a no-op for the files.
		before := tree.read(t, "sshd_config")
		if out, err := tree.run(t); err != nil || tree.read(t, "sshd_config") != before {
			t.Fatalf("second run changed files or failed: %v\n%s", err, out)
		}
	})
	t.Run("temp-file-failure", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "", true)
		writeExecutable(t, tree.bin, "mktemp", "exit 1")
		out, err := tree.run(t)
		if err == nil || !strings.Contains(out, "live config unchanged") {
			t.Fatalf("temp-file failure = %v %q, want a failure", err, out)
		}
		if tree.read(t, "sshd_config") != "Port 22\n" || tree.read(t, "sshd_config.d/99-serverpro.conf") != "<absent>" {
			t.Fatal("temp-file failure touched the live config")
		}
		if tree.stateFile("reloads") != "" {
			t.Fatal("temp-file failure reloaded ssh")
		}
	})
	t.Run("invalid-candidate-leaves-live-config", func(t *testing.T) {
		tree := newSSHDFixTree(t, "", "PermitRootLogin yes\n", true)
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config"), tree.includeMain())
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config.d", "50-other.conf"), "BADCONF yes\n")
		out, err := tree.run(t)
		if err == nil || !strings.Contains(out, "candidate config failed sshd -t; live config unchanged") {
			t.Fatalf("invalid candidate = %v %q", err, out)
		}
		if tree.read(t, "sshd_config.d/99-serverpro.conf") != "PermitRootLogin yes\n" || tree.read(t, "sshd_config") != tree.includeMain() {
			t.Fatal("invalid candidate was committed")
		}
		if tree.stateFile("reloads") != "" {
			t.Fatal("invalid candidate reloaded ssh")
		}
		assertNoLeftovers(t, tree)
	})
	t.Run("live-validation-failure-restores", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "", true)
		out, err := tree.run(t, "FAIL_LIVE_VALIDATION=1")
		if err == nil || !strings.Contains(out, "restored the previous files") {
			t.Fatalf("live validation failure = %v %q", err, out)
		}
		if tree.read(t, "sshd_config") != "Port 22\n" || tree.read(t, "sshd_config.d/99-serverpro.conf") != "<absent>" {
			t.Fatalf("live failure left a changed config: main=%q dropin=%q", tree.read(t, "sshd_config"), tree.read(t, "sshd_config.d/99-serverpro.conf"))
		}
		if tree.stateFile("reloads") != "" {
			t.Fatal("failed validation reloaded ssh")
		}
		assertNoLeftovers(t, tree)
	})
}

// TestSSHDSettingsFixFailSafeScript covers the paths that decide whether a
// broken repair can lock out new SSH logins or report success while the
// checks still fail.
func TestSSHDSettingsFixFailSafeScript(t *testing.T) {
	dropinPath := func(tree sshdFixTree) string { return filepath.Join(tree.sshDir, "sshd_config.d", "99-serverpro.conf") }
	t.Run("writes-dropin-before-main", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "", true)
		if out, err := tree.run(t); err != nil {
			t.Fatalf("fix failed: %v\n%s", err, out)
		}
		log := tree.stateFile("mv.log")
		dropin := strings.Index(log, " "+dropinPath(tree)+"\n")
		main := strings.Index(log, " "+filepath.Join(tree.sshDir, "sshd_config")+"\n")
		if dropin < 0 || main < 0 || dropin > main {
			t.Fatalf("the drop-in must land before the main config includes it:\n%s", log)
		}
	})
	t.Run("restores-existing-dropin", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "PermitRootLogin yes\n", true)
		out, err := tree.run(t, "FAIL_LIVE_VALIDATION=1")
		if exitCode(err) != 1 || !strings.Contains(out, "live config failed sshd -t after the write; restored the previous files") {
			t.Fatalf("live validation failure = %v %q", err, out)
		}
		if tree.read(t, "sshd_config") != "Port 22\n" || tree.read(t, "sshd_config.d/99-serverpro.conf") != "PermitRootLogin yes\n" {
			t.Fatalf("previous files not restored: main=%q dropin=%q", tree.read(t, "sshd_config"), tree.read(t, "sshd_config.d/99-serverpro.conf"))
		}
		assertNoLeftovers(t, tree)
	})
	t.Run("write-failure-restores", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "PermitRootLogin yes\n", true)
		out, err := tree.run(t, "FAIL_MV_MATCH=*sshd_config"+sshdStageSuffix)
		if exitCode(err) != 1 || !strings.Contains(out, "cannot write the main config; restored the previous files") {
			t.Fatalf("main write failure = %v %q", err, out)
		}
		if tree.read(t, "sshd_config") != "Port 22\n" || tree.read(t, "sshd_config.d/99-serverpro.conf") != "PermitRootLogin yes\n" {
			t.Fatal("a failed write left a changed config")
		}
		if tree.stateFile("reloads") != "" {
			t.Fatal("a failed write reloaded ssh")
		}
		assertNoLeftovers(t, tree)
	})
	t.Run("restore-failure-is-distinct", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "", true)
		out, err := tree.run(t, "FAIL_LIVE_VALIDATION=1", "FAIL_MV_MATCH=*sshd_config"+sshdRestoreSuffix)
		if exitCode(err) != sshdRestoreFailedExit || !strings.Contains(out, "restoring the previous files FAILED") || strings.Contains(out, "; restored the previous files") {
			t.Fatalf("restore failure = %v %q, want exit %d and no restored claim", err, out, sshdRestoreFailedExit)
		}
		assertNoLeftovers(t, tree)
	})
	t.Run("restored-files-still-invalid-is-distinct", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "", true)
		out, err := tree.run(t, "FAIL_LIVE_VALIDATION=always")
		if exitCode(err) != sshdRestoreFailedExit || !strings.Contains(out, "restored the previous files, but they fail sshd -t too") {
			t.Fatalf("still-invalid restore = %v %q, want exit %d", err, out, sshdRestoreFailedExit)
		}
		if tree.read(t, "sshd_config") != "Port 22\n" {
			t.Fatal("previous main config not restored")
		}
	})
	t.Run("signal-cleans-up-before-any-write", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Port 22\n", "", true)
		out, err := tree.run(t, "SIGNAL_ON_CANDIDATE=TERM")
		if err == nil {
			t.Fatalf("killed repair reported success: %q", out)
		}
		if tree.read(t, "sshd_config") != "Port 22\n" || tree.read(t, "sshd_config.d/99-serverpro.conf") != "<absent>" {
			t.Fatal("killed repair changed the live config")
		}
		assertNoLeftovers(t, tree)
	})
	t.Run("match-scoped-include-gains-top-level-include", func(t *testing.T) {
		tree := newSSHDFixTree(t, "", managedDropin(t), true)
		scoped := "Port 22\nMatch User nobody\n  Include " + tree.sshDir + "/sshd_config.d/*.conf\n"
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config"), scoped)
		if out, err := tree.run(t); err != nil {
			t.Fatalf("fix failed: %v\n%s", err, out)
		}
		if got, want := tree.read(t, "sshd_config"), "Include "+tree.sshDir+"/sshd_config.d/*.conf\n"+scoped; got != want {
			t.Fatalf("main config = %q, want %q", got, want)
		}
	})
	t.Run("relative-include-is-kept-and-validated-from-copy", func(t *testing.T) {
		tree := newSSHDFixTree(t, "Include sshd_config.d/*.conf\nPort 22\n", "PermitRootLogin yes\n", true)
		if out, err := tree.run(t); err != nil {
			t.Fatalf("fix failed: %v\n%s", err, out)
		}
		if got := tree.read(t, "sshd_config"); got != "Include sshd_config.d/*.conf\nPort 22\n" {
			t.Fatalf("relative Include gained a duplicate: %q", got)
		}
		if !regexp.MustCompile(`read \S+/tmp\.\w+/d/99-serverpro\.conf`).MatchString(tree.stateFile("sshd.log")) {
			t.Fatalf("candidate not validated against its drop-in copy:\n%s", tree.stateFile("sshd.log"))
		}
	})
	t.Run("overridden-dropin-fails-loudly", func(t *testing.T) {
		tree := newSSHDFixTree(t, "", "", true)
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config"), tree.includeMain())
		writeFile(t, filepath.Join(tree.sshDir, "sshd_config.d", "50-cloud-init.conf"), "PasswordAuthentication yes\n")
		out, err := tree.run(t)
		if exitCode(err) != 1 || !strings.Contains(out, "sshd -T lacks [passwordauthentication no]") {
			t.Fatalf("overridden drop-in = %v %q, want a failure naming the setting", err, out)
		}
		if tree.read(t, "sshd_config.d/99-serverpro.conf") != managedDropin(t) {
			t.Fatal("the valid drop-in should stay written")
		}
		assertNoLeftovers(t, tree)
	})
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func assertNoLeftovers(t *testing.T, tree sshdFixTree) {
	t.Helper()
	for _, pattern := range []string{filepath.Join(tree.root, "tmp.*"), filepath.Join(tree.sshDir, "*.serverpro-*"), filepath.Join(tree.sshDir, "sshd_config.d", "*.serverpro-*")} {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			t.Fatalf("temp files left behind: %v", matches)
		}
	}
}

// TestRemoteChecksWireCloudflaredRepairWithLongTimeout pins the connector
// repair into the doctor plan: it runs tunnel.UpgradeScript live, under the
// longer apt-sized deadline, and only when ingress uses the tunnel.
func TestRemoteChecksWireCloudflaredRepairWithLongTimeout(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "cloudflare-tunnel"
	plan := buildRemoteReadPlan(cfg)
	if !plan.allowsLive(tunnel.UpgradeScript()) {
		t.Fatal("cloudflared repair is not an allowed live command")
	}
	runner := &staleBatchRemote{
		baselineFail: map[string]string{tunnel.CheckCommand(): "cloudflared 2024.1.0 is below minimum"},
	}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{Fix: true})
	if res := resultByName(t, results, tunnel.CheckName); res.Status != Pass || !strings.Contains(res.Evidence, "fixed") {
		t.Fatalf("cloudflared = %+v", res)
	}
	deadline, ok := runner.deadlines[tunnel.UpgradeScript()]
	if !ok {
		t.Fatalf("cloudflared repair not run: %q", runner.runScripts)
	}
	if deadline < cloudflaredFixTimeout-time.Minute || deadline > cloudflaredFixTimeout {
		t.Fatalf("cloudflared repair deadline = %v, want about %v", deadline, cloudflaredFixTimeout)
	}
}
