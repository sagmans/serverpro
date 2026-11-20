package lifecycle

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/poll"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	// RebootStatusComplete means the host booted into a new boot ID and passed
	// any applicable post-reboot verification.
	RebootStatusComplete = "complete"
	// RebootStatusSkipped means --if-required was set and no reboot was pending.
	RebootStatusSkipped = "skipped"
	// RebootStatusRebooting means the reboot was scheduled and --no-wait skipped polling.
	RebootStatusRebooting = "rebooting"

	// RebootVerifyStatusOK means serverpro-reboot-verify confirmed all snapshot workloads returned.
	RebootVerifyStatusOK = "ok"
	// RebootVerifyStatusDegraded means one or more snapshot workloads failed to return.
	RebootVerifyStatusDegraded = "degraded"
	// RebootVerifyStatusStale means the snapshot could not safely describe the new boot.
	RebootVerifyStatusStale = "stale"
	// RebootVerifyStatusNotApplicable means the reboot did not use a rebootgate snapshot.
	RebootVerifyStatusNotApplicable = "not-applicable"

	rebootTriggerTimeout  = 5 * time.Minute
	rebootProbeTimeout    = 30 * time.Second
	rebootRecoveryTimeout = 10 * time.Minute
	rebootInitialGrace    = 10 * time.Second
	rebootPollInterval    = 5 * time.Second
	rebootScheduleDelay   = "2s"
	bootIDPath            = "/proc/sys/kernel/random/boot_id"

	markerRebootAction  = "SERVERPRO_REBOOT_ACTION="
	markerRebootGated   = "SERVERPRO_REBOOT_GATED="
	markerBootID        = "SERVERPRO_BOOT_ID="
	markerVerifyActive  = "SERVERPRO_VERIFY_ACTIVE="
	markerVerifyBoot    = "SERVERPRO_VERIFY_BOOT="
	markerVerifyStatus  = "SERVERPRO_VERIFY_STATUS="
	markerVerifyFinding = "SERVERPRO_VERIFY_FINDING="
	markerPreflightGap  = "SERVERPRO_PREFLIGHT_GAP="

	rebootActionSkipped   = "skipped"
	rebootActionRebooting = "rebooting"
	rebootActionBlocked   = "blocked"
)

// RebootPreflightError reports workload survivability gaps that prevented a
// gated reboot before systemctl reboot ran.
type RebootPreflightError struct {
	Host string
	Gaps []string
}

func (e *RebootPreflightError) Error() string {
	return fmt.Sprintf("reboot blocked on %s by %d survivability gap(s): %s", e.Host, len(e.Gaps), strings.Join(e.Gaps, "; "))
}

// RebootOptions controls an on-demand OS reboot over Tailscale SSH.
type RebootOptions struct {
	IfRequired bool
	Force      bool
	NoWait     bool
	Wait       poll.WaitFunc
}

// RebootResult reports what an on-demand reboot checked, triggered, and verified.
type RebootResult struct {
	Status                 string   `json:"status"`
	RebootRequired         bool     `json:"reboot_required"`
	RebootRequiredPackages []string `json:"reboot_required_packages,omitempty"`
	Rebooted               bool     `json:"rebooted"`
	Gated                  bool     `json:"gated"`
	BeforeBootID           string   `json:"before_boot_id,omitempty"`
	AfterBootID            string   `json:"after_boot_id,omitempty"`
	VerifyStatus           string   `json:"verify_status,omitempty"`
	VerifyFindings         []string `json:"verify_findings,omitempty"`
}

// RebootServer performs an OS-level reboot over Tailscale SSH. Unless Force is
// set, rebootgate preflight runs first and fails closed when any running
// workload is not declared to come back after reboot.
func RebootServer(ctx context.Context, r remote.Runner, cfg config.Config, st state.State, opts RebootOptions) (RebootResult, error) {
	if r == nil || st.Tailscale.Name == "" {
		return RebootResult{}, fmt.Errorf("remote host unavailable for reboot")
	}
	script, err := rebootTriggerScript(cfg.Reboot, opts)
	if err != nil {
		return RebootResult{}, fmt.Errorf("reboot script: %w", err)
	}
	out, err := remote.WithTimeout(r, rebootTriggerTimeout).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, script)
	if err != nil {
		return RebootResult{}, fmt.Errorf("reboot trigger: %w", err)
	}
	parsed, gaps, err := parseRebootTriggerOutput(st.Tailscale.Name, out)
	if err != nil {
		return parsed, err
	}
	if len(gaps) > 0 {
		return parsed, &RebootPreflightError{Host: st.Tailscale.Name, Gaps: gaps}
	}
	if parsed.Status == RebootStatusSkipped || opts.NoWait {
		return parsed, nil
	}
	return waitForRebootRecovery(ctx, r, cfg.Admin.Username, st.Tailscale.Name, parsed, opts.Wait)
}

func rebootTriggerScript(rebootCfg config.Reboot, opts RebootOptions) (string, error) {
	var b strings.Builder
	b.WriteString(maintenanceScriptHeader())
	b.WriteString("boot_id=$(cat " + shell.Quote(bootIDPath) + ")\n")
	b.WriteString("test -n \"$boot_id\" || { printf 'empty boot ID\\n' >&2; exit 1; }\n")
	b.WriteString("printf '%s%s\\n' '" + markerBootID + "' \"$boot_id\"\n")
	b.WriteString(emitRebootRequiredMarkers())
	if opts.IfRequired {
		b.WriteString("if [ ! -e " + shell.Quote(rebootgate.RebootRequiredPath) + " ]; then\n" +
			"  printf '%s\\n' '" + markerRebootAction + rebootActionSkipped + "'\n" +
			"  exit 0\n" +
			"fi\n")
	}
	if !opts.Force {
		preflight, err := rebootgate.PreflightProbe(rebootCfg, markerPreflightGap)
		if err != nil {
			return "", err
		}
		b.WriteString("preflight_status=0\n")
		b.WriteString("(\n" + preflight + ") || preflight_status=$?\n")
		b.WriteString("if [ \"$preflight_status\" -eq " + strconv.Itoa(rebootgate.GapExitCode) + " ]; then\n" +
			"  printf '%s\\n' '" + markerRebootAction + rebootActionBlocked + "'\n" +
			"  exit 0\n" +
			"fi\n" +
			"if [ \"$preflight_status\" -ne 0 ]; then exit \"$preflight_status\"; fi\n")
	}
	writeRebootScheduleBlock(&b, opts.Force)
	return b.String(), nil
}

// writeRebootScheduleBlock schedules the reboot via systemd-run with a short
// delay so the SSH command exits cleanly instead of racing TCP teardown.
func writeRebootScheduleBlock(b *strings.Builder, force bool) {
	unitExpr := rebootgate.OnDemandUnitPrefix + "$$"
	if !force {
		b.WriteString("if [ -e " + shell.Quote(rebootgate.RebootRequiredPath) + " ] && [ -x " + shell.Quote(rebootgate.ScriptPath) + " ]; then\n" +
			"  systemd-run --quiet --on-active=" + rebootScheduleDelay + " --unit=" + unitExpr +
			" --description='serverpro gated reboot' " + shell.Quote(rebootgate.ScriptPath) + " window\n" +
			"  printf '%s\\n' '" + markerRebootGated + markerTrueValue + "'\n" +
			"  printf '%s\\n' '" + markerRebootAction + rebootActionRebooting + "'\n" +
			"  exit 0\n" +
			"fi\n")
	}
	b.WriteString("systemd-run --quiet --on-active=" + rebootScheduleDelay + " --unit=" + unitExpr +
		" --description='serverpro requested reboot' /bin/systemctl reboot\n")
	b.WriteString("printf '%s\\n' '" + markerRebootGated + "0'\n")
	b.WriteString("printf '%s\\n' '" + markerRebootAction + rebootActionRebooting + "'\n")
}

func parseRebootTriggerOutput(host, out string) (RebootResult, []string, error) {
	res := RebootResult{}
	var action string
	var gaps []string
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case strings.HasPrefix(line, markerBootID):
			res.BeforeBootID = strings.TrimSpace(strings.TrimPrefix(line, markerBootID))
		case strings.HasPrefix(line, markerRebootRequired):
			res.RebootRequired = strings.TrimPrefix(line, markerRebootRequired) == markerTrueValue
		case strings.HasPrefix(line, markerRebootPkg):
			res.RebootRequiredPackages = appendNonEmpty(res.RebootRequiredPackages, strings.TrimPrefix(line, markerRebootPkg))
		case strings.HasPrefix(line, markerRebootGated):
			res.Gated = strings.TrimPrefix(line, markerRebootGated) == markerTrueValue
		case strings.HasPrefix(line, markerRebootAction):
			action = strings.TrimSpace(strings.TrimPrefix(line, markerRebootAction))
		case strings.HasPrefix(line, markerPreflightGap):
			gaps = appendNonEmpty(gaps, strings.TrimPrefix(line, markerPreflightGap))
		}
	}
	switch action {
	case rebootActionSkipped:
		res.Status = RebootStatusSkipped
		return res, nil, nil
	case rebootActionBlocked:
		return res, gaps, nil
	case rebootActionRebooting:
		if res.BeforeBootID == "" {
			return res, nil, fmt.Errorf("reboot trigger on %s omitted pre-reboot boot ID", host)
		}
		res.Status = RebootStatusRebooting
		res.Rebooted = true
		return res, nil, nil
	default:
		return res, nil, fmt.Errorf("reboot trigger on %s returned unexpected action %q", host, action)
	}
}

type rebootRecoveryProbe struct {
	BootID         string
	VerifyActive   bool
	VerifyBootID   string
	VerifyStatus   string
	VerifyFindings []string
}

func waitForRebootRecovery(ctx context.Context, r remote.Runner, user, host string, res RebootResult, wait poll.WaitFunc) (RebootResult, error) {
	recoveryCtx, cancel := context.WithTimeout(ctx, rebootRecoveryTimeout)
	defer cancel()
	if err := poll.Wait(recoveryCtx, wait, rebootInitialGrace); err != nil {
		return res, fmt.Errorf("waiting for %s to reboot: %w", host, err)
	}
	probeScript := rebootRecoveryProbeScript()
	for {
		out, err := remote.WithTimeout(r, rebootProbeTimeout).Run(recoveryCtx, user, host, probeScript)
		if err == nil {
			probe := parseRebootRecoveryProbe(out)
			if done, finalRes, verifyErr := evaluateRebootProbe(host, res, probe); done {
				return finalRes, verifyErr
			}
		}
		if err := poll.Wait(recoveryCtx, wait, rebootPollInterval); err != nil {
			return res, fmt.Errorf("waiting for %s to return after reboot: %w", host, err)
		}
	}
}

func evaluateRebootProbe(host string, res RebootResult, probe rebootRecoveryProbe) (bool, RebootResult, error) {
	if probe.BootID == "" || probe.BootID == res.BeforeBootID {
		return false, res, nil
	}
	res.AfterBootID = probe.BootID
	if !res.Gated {
		res.Status = RebootStatusComplete
		res.VerifyStatus = RebootVerifyStatusNotApplicable
		return true, res, nil
	}
	if probe.VerifyActive || probe.VerifyBootID != probe.BootID {
		return false, res, nil
	}
	res.VerifyStatus = probe.VerifyStatus
	res.VerifyFindings = probe.VerifyFindings
	if probe.VerifyStatus == RebootVerifyStatusOK {
		res.Status = RebootStatusComplete
		return true, res, nil
	}
	return true, res, fmt.Errorf("post-reboot verification on %s reported %s: %s", host, probe.VerifyStatus, strings.Join(probe.VerifyFindings, "; "))
}

func rebootRecoveryProbeScript() string {
	return maintenanceScriptHeader() +
		"boot_id=$(cat " + shell.Quote(bootIDPath) + ")\n" +
		"printf '%s%s\\n' '" + markerBootID + "' \"$boot_id\"\n" +
		"verify_state=$(systemctl is-active " + shell.Quote(rebootgate.VerifyUnit) + " 2>/dev/null || true)\n" +
		"case \"$verify_state\" in\n" +
		"  active|activating) printf '%s\\n' '" + markerVerifyActive + markerTrueValue + "' ;;\n" +
		"  *) printf '%s\\n' '" + markerVerifyActive + "0' ;;\n" +
		"esac\n" +
		"if [ -r " + shell.Quote(rebootgate.ResultPath) + " ]; then\n" +
		"  sed -n 's/^boot=/" + markerVerifyBoot + "/p' " + shell.Quote(rebootgate.ResultPath) + "\n" +
		"  sed -n 's/^status=/" + markerVerifyStatus + "/p' " + shell.Quote(rebootgate.ResultPath) + "\n" +
		"  sed -n 's/^finding=/" + markerVerifyFinding + "/p' " + shell.Quote(rebootgate.ResultPath) + "\n" +
		"fi\n"
}

func parseRebootRecoveryProbe(out string) rebootRecoveryProbe {
	var probe rebootRecoveryProbe
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case strings.HasPrefix(line, markerBootID):
			probe.BootID = strings.TrimSpace(strings.TrimPrefix(line, markerBootID))
		case strings.HasPrefix(line, markerVerifyActive):
			probe.VerifyActive = strings.TrimPrefix(line, markerVerifyActive) == markerTrueValue
		case strings.HasPrefix(line, markerVerifyBoot):
			probe.VerifyBootID = strings.TrimSpace(strings.TrimPrefix(line, markerVerifyBoot))
		case strings.HasPrefix(line, markerVerifyStatus):
			probe.VerifyStatus = strings.TrimSpace(strings.TrimPrefix(line, markerVerifyStatus))
		case strings.HasPrefix(line, markerVerifyFinding):
			probe.VerifyFindings = appendNonEmpty(probe.VerifyFindings, strings.TrimPrefix(line, markerVerifyFinding))
		}
	}
	return probe
}
