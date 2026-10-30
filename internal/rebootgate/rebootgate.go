// Package rebootgate renders the host side of serverpro's gated reboot: the
// serverpro-reboot script, its timer and units, and the install script that
// puts them on a host. Unattended upgrades install kernel and library fixes
// but leave the reboot that loads them to this gate, which reboots only when
// every running workload is declared to come back and checks after boot that
// it did. Provisioning and doctor --fix install the same files, and doctor
// compares a host with them.
package rebootgate

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/shell"
)

//go:embed serverpro-reboot.sh
var scriptBody string

const (
	// LogTag names the script, the units, and every journald entry, so one
	// journalctl -t filter shows all of the gate's decisions.
	LogTag     = "serverpro-reboot"
	ScriptPath = "/usr/local/sbin/" + LogTag
	unitDir    = "/etc/systemd/system"
	TimerUnit  = LogTag + ".timer"
	// ServiceUnit has no [Install] section: only the timer starts it.
	ServiceUnit = LogTag + ".service"
	VerifyUnit  = LogTag + "-verify.service"
	// RebootRequiredPath is the flag Ubuntu's update hooks write when an
	// installed update needs a reboot.
	RebootRequiredPath = "/var/run/reboot-required"
	// stateDir is serverpro's root-owned host state directory, which
	// cloud-init creates for the bootstrap record.
	stateDir     = "/var/lib/serverpro"
	SnapshotPath = stateDir + "/reboot-snapshot"
	ResultPath   = stateDir + "/reboot-verify.result"
	// lingerDir holds one file per user whose systemd manager runs without a
	// login, which logind reads at boot.
	lingerDir = "/var/lib/systemd/linger"

	// InstalledGapPrefix starts each gap line an operator reads from
	// serverpro-reboot preflight; doctor renders its own prefix instead.
	InstalledGapPrefix = "gap: "
	// WarningPrefix starts each install warning. It equals the doctor drift
	// warning prefix, so doctor --fix reports the warning after a repair
	// succeeds.
	WarningPrefix = "warn: "
	// GapExitCode separates "gaps found" from a preflight that could not
	// read the host, which exits 1.
	GapExitCode = 3

	// commandTimeout bounds each systemctl, docker, or ss call; a hung call
	// must not hold the window or the boot check.
	commandTimeout   = 30 * time.Second
	commandKillGrace = 5 * time.Second
	// verifyPoll is how often the boot check looks at workloads while it
	// waits for them.
	verifyPoll = 10 * time.Second
	// restartTimeout bounds one restart of a missing unit or container, and
	// restartBudget bounds all of them, so the boot check always ends.
	restartTimeout = 90 * time.Second
	restartBudget  = 10 * time.Minute
	// restartSettle is the wait after the restarts for listeners and health
	// checks of the restarted workloads.
	restartSettle = time.Minute
	// verifyGrace lets the script write its result before systemd stops the
	// unit at its runtime limit.
	verifyGrace = time.Minute
	// windowTimeout bounds the preflight and snapshot before a reboot.
	windowTimeout = 5 * time.Minute
	// snapshotMaxAge is how old a snapshot may be when the boot check reads
	// it. A gated reboot takes minutes; an older snapshot means this boot
	// followed some later reboot, and comparing with it could start a
	// workload the operator stopped since.
	snapshotMaxAge = time.Hour

	scriptMode fs.FileMode = 0o755
	unitMode   fs.FileMode = 0o644
)

// File is one file the gate installs, with the exact bytes and mode doctor
// expects on the host.
type File struct {
	Path    string
	Mode    fs.FileMode
	Content string
}

// SHA256 is the digest doctor compares with the host file, so a hand edit
// to the script or a unit reads as drift.
func (f File) SHA256() string {
	sum := sha256.Sum256([]byte(f.Content))
	return hex.EncodeToString(sum[:])
}

// settingsBlock renders the values the script body reads. Every value comes
// from config or a constant here, so the body holds no setting of its own.
func settingsBlock(r config.Reboot, gapPrefix string) (string, error) {
	verifyTimeout, err := r.VerifyTimeoutDuration()
	if err != nil {
		return "", err
	}
	settings := [][2]string{
		{"LOG_TAG", LogTag},
		{"GAP_PREFIX", gapPrefix},
		{"GAP_EXIT_CODE", strconv.Itoa(GapExitCode)},
		{"REBOOT_REQUIRED_PATH", RebootRequiredPath},
		{"STATE_DIR", stateDir},
		{"SNAPSHOT_PATH", SnapshotPath},
		{"RESULT_PATH", ResultPath},
		{"LINGER_DIR", lingerDir},
		{"COMMAND_TIMEOUT_SECONDS", seconds(commandTimeout)},
		{"COMMAND_KILL_GRACE_SECONDS", seconds(commandKillGrace)},
		{"VERIFY_TIMEOUT_SECONDS", seconds(verifyTimeout)},
		{"VERIFY_POLL_SECONDS", seconds(verifyPoll)},
		{"RESTART_TIMEOUT_SECONDS", seconds(restartTimeout)},
		{"RESTART_BUDGET_SECONDS", seconds(restartBudget)},
		{"RESTART_SETTLE_SECONDS", seconds(restartSettle)},
		{"SNAPSHOT_MAX_AGE_SECONDS", seconds(snapshotMaxAge)},
	}
	var block strings.Builder
	for _, setting := range settings {
		block.WriteString(setting[0] + "=" + shell.Quote(setting[1]) + "\n")
	}
	return block.String(), nil
}

func seconds(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10)
}

// HostScript is the installed serverpro-reboot, which takes its mode as the
// first argument.
func HostScript(r config.Reboot) (string, error) {
	settings, err := settingsBlock(r, InstalledGapPrefix)
	if err != nil {
		return "", err
	}
	return "#!/bin/sh\n" + settings + scriptBody, nil
}

// PreflightProbe renders the preflight to run in a subshell of a larger
// probe: doctor sends it inline, so the check works before the gate is
// installed and on a host where it is turned off. Each gap line starts
// with gapPrefix, and the subshell exits GapExitCode when it found gaps.
func PreflightProbe(r config.Reboot, gapPrefix string) (string, error) {
	settings, err := settingsBlock(r, gapPrefix)
	if err != nil {
		return "", err
	}
	return "(\nset -- preflight\n" + settings + scriptBody + ")\n", nil
}

// Files returns everything the gate installs, in install order.
func Files(r config.Reboot) ([]File, error) {
	script, err := HostScript(r)
	if err != nil {
		return nil, err
	}
	hour, minute, err := r.WindowClock()
	if err != nil {
		return nil, err
	}
	verifyTimeout, err := r.VerifyTimeoutDuration()
	if err != nil {
		return nil, err
	}
	return []File{
		{Path: ScriptPath, Mode: scriptMode, Content: script},
		{Path: unitDir + "/" + ServiceUnit, Mode: unitMode, Content: serviceUnit()},
		{Path: unitDir + "/" + TimerUnit, Mode: unitMode, Content: timerUnit(hour, minute)},
		{Path: unitDir + "/" + VerifyUnit, Mode: unitMode, Content: verifyUnit(verifyTimeout)},
	}, nil
}

// serviceUnit runs only while a reboot is pending, so a quiet night leaves
// no log entry behind.
func serviceUnit() string {
	return "[Unit]\n" +
		"Description=serverpro gated reboot for pending updates\n" +
		"ConditionPathExists=" + RebootRequiredPath + "\n" +
		"\n" +
		"[Service]\n" +
		"Type=oneshot\n" +
		"ExecStart=" + ScriptPath + " window\n" +
		"TimeoutStartSec=" + seconds(windowTimeout) + "\n"
}

// timerUnit fires once a day in the window. Persistent=false keeps a host
// that was off during the window from rebooting as soon as it boots.
func timerUnit(hour, minute int) string {
	return "[Unit]\n" +
		"Description=serverpro gated reboot window\n" +
		"\n" +
		"[Timer]\n" +
		fmt.Sprintf("OnCalendar=*-*-* %02d:%02d:00 UTC\n", hour, minute) +
		"Persistent=false\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=timers.target\n"
}

// verifyUnit runs after boot only when a gated reboot left a snapshot.
// Type=exec lets boot finish while it waits for workloads, and
// RuntimeMaxSec stops it if the script's own deadlines fail.
func verifyUnit(verifyTimeout time.Duration) string {
	return "[Unit]\n" +
		"Description=serverpro check that workloads came back after a gated reboot\n" +
		"After=multi-user.target docker.service\n" +
		"ConditionPathExists=" + SnapshotPath + "\n" +
		"\n" +
		"[Service]\n" +
		"Type=exec\n" +
		"ExecStart=" + ScriptPath + " verify\n" +
		"RuntimeMaxSec=" + seconds(verifyTimeout+restartBudget+restartSettle+verifyGrace) + "\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=multi-user.target\n"
}

// InstallScript writes the files and turns the timer on. It rewrites only a
// file whose bytes differ, so a rerun on a converged host changes nothing.
// When a reboot is already pending it warns, because the next window can
// reboot the host.
func InstallScript(r config.Reboot) (string, error) {
	files, err := Files(r)
	if err != nil {
		return "", err
	}
	var script strings.Builder
	script.WriteString(`set -eu
umask 022
changed=0
write_managed() {
  tmp=$(mktemp "$(dirname "$1")/.` + LogTag + `.XXXXXX")
  trap 'rm -f "$tmp"' EXIT
  printf '%s' "$3" >"$tmp"
  chmod "$2" "$tmp"
  chown root:root "$tmp"
  if cmp -s "$tmp" "$1"; then
    rm -f "$tmp"
    chmod "$2" "$1"
    chown root:root "$1"
  else
    mv -f "$tmp" "$1"
    changed=1
  fi
  trap - EXIT
}
`)
	for _, file := range files {
		fmt.Fprintf(&script, "write_managed %s %04o %s\n", shell.Quote(file.Path), file.Mode, shell.Quote(file.Content))
	}
	script.WriteString("systemctl daemon-reload\n" +
		"systemctl enable " + VerifyUnit + "\n" +
		"systemctl enable " + TimerUnit + "\n" +
		// A changed timer must re-arm with its new calendar.
		"if [ \"$changed\" = 1 ]; then systemctl restart " + TimerUnit + "; else systemctl start " + TimerUnit + "; fi\n" +
		"if [ -e " + RebootRequiredPath + " ]; then\n" +
		"  printf '%s\\n' " + shell.Quote(WarningPrefix+"a reboot is pending; the gated reboot runs at "+r.Window+" UTC when its preflight finds no gap") + "\n" +
		"fi\n")
	return script.String(), nil
}
