package doctor

import (
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	rebootSurvivabilityCheckName = "reboot survivability"
	// rebootInstallFinding marks the findings --fix can repair: the gate's
	// own files and units. Gaps name workloads, which --fix never changes.
	rebootInstallFinding = "install: "
	// rebootSurvivabilityRemediation names operator commands only: doctor
	// never reboots, and never rewrites a workload's restart policy or
	// unit, because only the operator knows whether that workload should
	// come back.
	rebootSurvivabilityRemediation = "serverpro server doctor --fix installs or repairs only the gated reboot's script, timer, and units; it never reboots and never changes a workload. For each gap, run the command it names (such as docker update --restart unless-stopped NAME, or sudo systemctl enable UNIT), or move a process started by hand into a service, then rerun doctor"
	// needrestartPath is reported, not required: it restarts services after
	// library updates, which shortens the time a reboot is pending.
	needrestartPath = "/usr/sbin/needrestart"
	// bootIDPath changes on every boot, which ties a post-reboot result to
	// the boot it checked.
	bootIDPath = "/proc/sys/kernel/random/boot_id"
)

// remoteRebootSurvivabilitySpecification runs the gate's preflight on every
// host, because any reboot (gated, manual, or a provider's maintenance) drops
// a workload that is not declared to come back. With the gate on, --fix
// installs or repairs it; that is how an older host gets it, never silently.
func remoteRebootSurvivabilitySpecification(reboot config.Reboot) remoteCheckSpecification {
	command, err := rebootSurvivabilityCommand(reboot)
	if err != nil {
		return remoteStaticSpecification(fail("remote", rebootSurvivabilityCheckName, err.Error(), "fix the reboot section of the server config"))
	}
	if !reboot.Enabled {
		return remoteDriftSpecification(rebootSurvivabilityCheckName, command, rebootSurvivabilityRemediation)
	}
	install, err := rebootgate.InstallScript(reboot)
	if err != nil {
		return remoteStaticSpecification(fail("remote", rebootSurvivabilityCheckName, err.Error(), "fix the reboot section of the server config"))
	}
	return remoteFixableDriftSpecificationWhen(rebootSurvivabilityCheckName, command, rebootSurvivabilityRemediation, install, func(result Result) bool {
		return strings.Contains(result.Evidence, rebootInstallFinding)
	})
}

// rebootSurvivabilityCommand compares the installed gate with what config
// renders, runs the preflight inline so it works before the gate is
// installed, and reports the last post-reboot check while the host still
// runs the boot it checked.
func rebootSurvivabilityCommand(reboot config.Reboot) (string, error) {
	preflight, err := rebootgate.PreflightProbe(reboot, driftWarnPrefix)
	if err != nil {
		return "", err
	}
	var command strings.Builder
	command.WriteString("export LC_ALL=C\n")
	if reboot.Enabled {
		installed, err := rebootInstallCommand(reboot)
		if err != nil {
			return "", err
		}
		command.WriteString(installed)
	} else {
		command.WriteString("if [ \"$(systemctl is-enabled " + rebootgate.TimerUnit + " 2>/dev/null)\" = enabled ]; then\n" +
			"  printf '%s\\n' " + shell.Quote(driftWarnPrefix+rebootgate.TimerUnit+" is enabled while config turns the gated reboot off: sudo systemctl disable --now "+rebootgate.TimerUnit) + "\n" +
			"fi\n" +
			"printf '%s\\n' 'gated reboot off in config'\n")
	}
	command.WriteString(preflight)
	command.WriteString("status=$?\n" +
		"if [ \"$status\" -ne 0 ] && [ \"$status\" -ne " + strconv.Itoa(rebootgate.GapExitCode) + " ]; then exit \"$status\"; fi\n")
	command.WriteString(rebootResultCommand())
	command.WriteString("if [ -x " + needrestartPath + " ]; then printf '%s\\n' 'needrestart present'; else printf '%s\\n' 'needrestart absent'; fi\n")
	return command.String(), nil
}

// rebootInstallCommand fails each file whose bytes, mode, or owner differ
// from the rendered copy and each unit that is not enabled, so --fix has a
// precise finding to repair.
func rebootInstallCommand(reboot config.Reboot) (string, error) {
	files, err := rebootgate.Files(reboot)
	if err != nil {
		return "", err
	}
	var command strings.Builder
	command.WriteString(`check_gate_file() {
  if [ ! -f "$1" ]; then
    printf '` + driftFailPrefix + rebootInstallFinding + `%s is missing\n' "$1"
    return 0
  fi
  [ "$(sha256sum "$1" | cut -d' ' -f1)" = "$3" ] || printf '` + driftFailPrefix + rebootInstallFinding + `%s differs from the managed copy\n' "$1"
  [ "$(stat -c '%a %U' "$1")" = "$2 root" ] || printf '` + driftFailPrefix + rebootInstallFinding + `%s mode or owner differs from %s root\n' "$1" "$2"
}
check_gate_unit() {
  state=$(systemctl "$2" "$1" 2>/dev/null)
  [ "$state" = "$3" ] || printf '` + driftFailPrefix + rebootInstallFinding + `%s is %s, not %s\n' "$1" "${state:-unknown}" "$3"
}
`)
	for _, file := range files {
		command.WriteString("check_gate_file " + shell.Quote(file.Path) + " " + strconv.FormatUint(uint64(file.Mode.Perm()), 8) + " " + file.SHA256() + "\n")
	}
	command.WriteString("check_gate_unit " + rebootgate.TimerUnit + " is-enabled enabled\n" +
		"check_gate_unit " + rebootgate.TimerUnit + " is-active active\n" +
		"check_gate_unit " + rebootgate.VerifyUnit + " is-enabled enabled\n" +
		"printf '%s\\n' " + shell.Quote("gated reboot window "+reboot.Window+" UTC") + "\n")
	return command.String(), nil
}

// rebootResultCommand warns about a post-reboot check that was not ok only
// while the host runs the boot it checked; after a later reboot the finding
// no longer describes the host. It also warns when a gated reboot left a
// snapshot that no check works on, or the check failed before it wrote a
// result for this boot: either way the boot was never compared with the
// snapshot, and the result file alone would not show it.
func rebootResultCommand() string {
	return "current_boot=$(cat " + bootIDPath + ")\n" +
		"result_boot=\n" +
		"if [ -r " + rebootgate.ResultPath + " ]; then\n" +
		"  result_time=$(sed -n 's/^time=//p' " + rebootgate.ResultPath + ")\n" +
		"  result_boot=$(sed -n 's/^boot=//p' " + rebootgate.ResultPath + ")\n" +
		"  result_status=$(sed -n 's/^status=//p' " + rebootgate.ResultPath + ")\n" +
		"  if [ \"$result_status\" != ok ] && [ \"$result_boot\" = \"$current_boot\" ]; then\n" +
		"    findings=$(sed -n 's/^finding=//p' " + rebootgate.ResultPath + " | awk 'NR > 1 { printf \"" + driftEvidenceSeparator + "\" } { printf \"%s\", $0 }')\n" +
		"    printf '" + driftWarnPrefix + "the post-reboot check at %s was %s: %s\\n' \"$result_time\" \"$result_status\" \"$findings\"\n" +
		"  else\n" +
		"    printf 'last post-reboot check %s at %s\\n' \"${result_status:-unknown}\" \"${result_time:-unknown}\"\n" +
		"  fi\n" +
		"fi\n" +
		"if [ -e " + rebootgate.SnapshotPath + " ]; then\n" +
		"  case \"$(systemctl is-active " + rebootgate.VerifyUnit + " 2>/dev/null)/$(systemctl is-active " + rebootgate.ServiceUnit + " 2>/dev/null)\" in\n" +
		"    active/* | activating/*) printf '%s\\n' 'post-reboot check running' ;;\n" +
		"    */active | */activating) printf '%s\\n' 'gated reboot window running' ;;\n" +
		"    *) printf '%s\\n' " + shell.Quote(driftWarnPrefix+"a gated reboot snapshot exists, but no post-reboot check runs: the check did not finish, or the reboot did not happen; journalctl -t "+rebootgate.LogTag+" shows why") + " ;;\n" +
		"  esac\n" +
		"elif [ \"$(systemctl is-failed " + rebootgate.VerifyUnit + " 2>/dev/null)\" = failed ] && [ \"$result_boot\" != \"$current_boot\" ]; then\n" +
		"  printf '%s\\n' " + shell.Quote(driftWarnPrefix+"the post-reboot check failed before it wrote a result for this boot: journalctl -b -u "+rebootgate.VerifyUnit) + "\n" +
		"fi\n"
}
