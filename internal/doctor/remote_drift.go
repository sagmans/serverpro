package doctor

import (
	"context"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/network"
	"github.com/sagmans/serverpro/internal/remote"
)

const (
	// driftProbeFailedRemediation covers a probe that could not read the
	// host: drift is unknown then, which doctor must not report as a pass.
	driftProbeFailedRemediation = "the read-only probe could not run; check Tailscale SSH and sudo, then rerun doctor"
	driftEvidenceSeparator      = "; "
)

// remoteDriftSpecifications are checks for host drift that the hardening
// checks cannot see: each compares the live host with what config or
// provisioning promised. Most are read-only, because the finding needs an
// operator decision (an unknown account, a workload a reboot would drop).
// Restricted egress, OpenSSH off, the unattended-upgrades drop-in, the gated
// reboot's own units, and provider image accounts have a repair, because
// config, provisioning, or the provider adapter already states the wanted
// result.
func remoteDriftSpecifications(cfg config.Config, opt Options) []remoteCheckSpecification {
	user := cfg.Admin.Username
	var specifications []remoteCheckSpecification
	if cfg.Network.Egress.LockdownDue() {
		specifications = append(specifications, remoteFixableDriftSpecification(egressRestrictedCheckName, egressRestrictedCommand(), egressRestrictedRemediation, network.LockdownScript(cfg)))
	}
	return append(specifications,
		remoteOpenSSHSpecification(cfg, opt.SSHConsoleAgents),
		remoteDriftSpecification(rebootPendingCheckName, rebootPendingCommand(), rebootPendingRemediationFor(cfg)),
		remoteRebootSurvivabilitySpecification(cfg.Reboot),
		remoteFixableDriftSpecification(unattendedUpgradesConfigCheckName, unattendedUpgradesConfigCommand(), unattendedUpgradesConfigRemediation, unattendedUpgradesConfigFixCommand()),
		remotePrivilegedAccountsSpecification(user, opt.ImageDefaultUsers),
		remoteMetadataUserDataSpecification(user, opt.MetadataUserDataPath),
		remoteDriftSpecification(tailscaleOperatorCheckName, tailscaleOperatorCommand(), tailscaleOperatorRemediation),
		remoteTailscaleServeSpecification(),
		remoteDriftSpecification(sysctlRuntimeCheckName, sysctlRuntimeCommand(), sysctlRuntimeRemediation),
	)
}

// remoteMetadataUserDataSpecification reports a skip, not a pass, when the
// provider gives no path: an unprobed endpoint must not read as a clean one.
func remoteMetadataUserDataSpecification(user, path string) remoteCheckSpecification {
	if path == "" {
		return remoteStaticSpecification(skip("remote", metadataUserDataCheckName, metadataUserDataUnchecked))
	}
	return remoteDriftSpecification(metadataUserDataCheckName, metadataUserDataCommand(user, path), metadataUserDataRemediation)
}

func remoteDriftSpecification(name, command, remediation string) remoteCheckSpecification {
	return remoteGradedDriftSpecification(name, command, nil, remediation)
}

// outputGrader turns raw probe output into drift lines for checks whose
// probe returns data Go grades, such as JSON. An error means the output
// could not be read and fails the check as a probe failure.
type outputGrader func(string) (string, error)

// remoteGradedDriftSpecification is remoteDriftSpecification for a probe
// whose output passes through grade first; a nil grade passes it as is.
func remoteGradedDriftSpecification(name, command string, grade outputGrader, remediation string) remoteCheckSpecification {
	return remoteCheckSpecification{
		readCommands: []string{command},
		run: func(ctx context.Context, runner remote.Runner, user, host string, _ Options) []Result {
			out, err := gradedRun(ctx, runner, user, host, command, grade)
			return []Result{driftResult(name, out, err, remediation)}
		},
	}
}

// gradedRun runs the probe and grades its output. A grade error counts as a
// failed probe, so it never reaches a repair, and it replaces the raw
// output, which may be a whole sudoers document.
func gradedRun(ctx context.Context, runner remote.Runner, user, host, command string, grade outputGrader) (string, error) {
	out, err := runner.Run(ctx, user, host, command)
	if err == nil && grade != nil {
		return grade(out)
	}
	return out, err
}

// remotePrivilegedAccountsSpecification repairs only what the compute
// provider declared as its own image accounts. Any other extra account
// (a deploy user, a contractor's login) may run a workload, so it stays
// report-only even with --fix. With no declared accounts, or a declaration
// the removal script refuses, the check has no repair at all.
func remotePrivilegedAccountsSpecification(admin string, imageUsers []string) remoteCheckSpecification {
	command := privilegedAccountsCommand(admin, imageUsers)
	grade := gradePrivilegedAccounts(admin)
	removal, err := lifecycle.ImageUsersRemovalScript(admin, imageUsers)
	if len(imageUsers) == 0 || err != nil {
		return remoteGradedDriftSpecification(privilegedAccountsCheckName, command, grade, privilegedAccountsRemediation)
	}
	return remoteGradedFixableDriftSpecification(privilegedAccountsCheckName, command, grade, privilegedAccountsRemediation, removal, func(result Result) bool {
		return namesAnyAccount(result.Evidence, imageUsers)
	}, nil)
}

// namesAnyAccount reports whether evidence names one of the accounts as a
// whole word, so "linuxuser2" does not count as "linuxuser".
func namesAnyAccount(evidence string, accounts []string) bool {
	words := strings.FieldsFunc(evidence, func(r rune) bool { return !isAccountNameRune(r) })
	for _, word := range words {
		for _, account := range accounts {
			if word == account {
				return true
			}
		}
	}
	return false
}

// isAccountNameRune matches the characters the image account names may use
// (lifecycle's portable account-name set), so the split keeps names whole.
func isAccountNameRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-'
}

// remoteFixableDriftSpecification gives a drift check a repair that
// converges the host back to config. The repair runs only on a finding the
// probe could read: a probe that never ran leaves the host state unknown, and
// a repair on an unknown state could undo an operator's deliberate change.
// The check re-reads live after another check ran the same repair, so a
// shared repair settles every check that depends on it.
func remoteFixableDriftSpecification(name, command, remediation, fixCommand string) remoteCheckSpecification {
	return remoteFixableDriftSpecificationWhen(name, command, remediation, fixCommand, nil)
}

// remoteFixableDriftSpecificationWhen adds a gate for checks whose repair
// covers only some findings: fixable sees the drift result and decides
// whether the repair can change it. A nil gate repairs any drift.
func remoteFixableDriftSpecificationWhen(name, command, remediation, fixCommand string, fixable func(Result) bool) remoteCheckSpecification {
	return remoteGuardedFixableDriftSpecification(name, command, remediation, fixCommand, fixable, nil)
}

// remoteGuardedFixableDriftSpecification adds a preflight that runs off the
// host, right before the repair, for a repair whose safety depends on state
// the host cannot see. An error keeps the repair from running and becomes
// the finding's evidence. A nil guard always lets the repair run.
func remoteGuardedFixableDriftSpecification(name, command, remediation, fixCommand string, fixable func(Result) bool, guard func(context.Context, Options) error) remoteCheckSpecification {
	return remoteGradedFixableDriftSpecification(name, command, nil, remediation, fixCommand, fixable, guard)
}

// remoteGradedFixableDriftSpecification is the shared body: the probe output
// passes through grade (see gradedDriftResult) before and after the repair.
func remoteGradedFixableDriftSpecification(name, command string, grade outputGrader, remediation, fixCommand string, fixable func(Result) bool, guard func(context.Context, Options) error) remoteCheckSpecification {
	return remoteCheckSpecification{
		readCommands:     []string{command},
		liveCommands:     []string{fixCommand},
		refreshAfterLive: []string{fixCommand},
		run: func(ctx context.Context, runner remote.Runner, user, host string, opt Options) []Result {
			if opt.refreshBaseline {
				// The batch baseline predates a repair earlier in this run.
				discardBaseline(runner, command)
			}
			out, err := gradedRun(ctx, runner, user, host, command, grade)
			result := driftResult(name, out, err, remediation)
			if err != nil || result.Status == Pass || !opt.Fix || (fixable != nil && !fixable(result)) {
				return []Result{result}
			}
			if opt.fixRan != nil && opt.fixRan(fixCommand) {
				return []Result{fail("remote", name, result.Evidence+repairAlreadyRanEvidence, repairAlreadyRanRemediation)}
			}
			if guard != nil {
				if err := guard(ctx, opt); err != nil {
					return []Result{fail("remote", name, result.Evidence+"; fix not run: "+err.Error(), remediation)}
				}
			}
			fixOut, fixErr := runner.Run(ctx, user, host, fixCommand)
			if fixErr != nil {
				return []Result{fail("remote", name, result.Evidence+"; fix failed: "+remoteFailureEvidence(fixOut, fixErr), remediation)}
			}
			out, err = gradedRun(ctx, runner, user, host, command, grade)
			after := driftResult(name, out, err, remediation)
			if after.Status == Pass {
				return []Result{fixedResult(name, after.Evidence, fixOut, remediation)}
			}
			if err == nil && fixable != nil && !fixable(after) {
				// The repair settled every finding it covers. What is left is
				// report-only, so it keeps its own severity instead of reading
				// as a repair that failed.
				after.Evidence = repairPartialEvidence + after.Evidence
				return []Result{after}
			}
			return []Result{fail("remote", name, "fix applied but drift remains: "+after.Evidence, remediation)}
		},
	}
}

// fixedResult keeps the warnings a repair prints: a repair can succeed and
// still leave the operator something to act on, such as a recovery path the
// change removed, and dropping them would hide it behind a pass.
func fixedResult(name, evidence, fixOut, remediation string) Result {
	evidence = "fixed: " + evidence
	var warnings []string
	for _, line := range strings.Split(fixOut, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, driftWarnPrefix) {
			warnings = append(warnings, strings.TrimPrefix(line, driftWarnPrefix))
		}
	}
	if len(warnings) == 0 {
		return pass("remote", name, evidence)
	}
	result := warn("remote", name, evidence+driftEvidenceSeparator+strings.Join(warnings, driftEvidenceSeparator))
	result.Remediation = remediation
	return result
}

// driftResult turns probe output into one result: any fail line fails, any
// warn line warns, and the remaining lines become pass evidence.
func driftResult(name, out string, err error, remediation string) Result {
	if err != nil {
		return fail("remote", name, remoteFailureEvidence(out, err), driftProbeFailedRemediation)
	}
	var failures, warnings, details []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, driftFailPrefix):
			failures = append(failures, strings.TrimPrefix(line, driftFailPrefix))
		case strings.HasPrefix(line, driftWarnPrefix):
			warnings = append(warnings, strings.TrimPrefix(line, driftWarnPrefix))
		default:
			details = append(details, line)
		}
	}
	findings := append(append([]string(nil), failures...), warnings...)
	switch {
	case len(failures) > 0:
		return fail("remote", name, strings.Join(findings, driftEvidenceSeparator), remediation)
	case len(warnings) > 0:
		result := warn("remote", name, strings.Join(findings, driftEvidenceSeparator))
		result.Remediation = remediation
		return result
	case len(details) == 0:
		return pass("remote", name, "ok")
	}
	return pass("remote", name, strings.Join(details, driftEvidenceSeparator))
}

// remoteTailscaleServeSpecification parses the serve config in Go; see
// TailscaleServeCommand for why one read carries both documents.
func remoteTailscaleServeSpecification() remoteCheckSpecification {
	command := TailscaleServeCommand()
	return remoteCheckSpecification{
		readCommands: []string{command},
		run: func(ctx context.Context, runner remote.Runner, user, host string, _ Options) []Result {
			out, err := runner.Run(ctx, user, host, command)
			if err != nil {
				return []Result{fail("remote", tailscaleServeCheckName, withoutServeConfig(remoteFailureEvidence(out, err)), driftProbeFailedRemediation)}
			}
			return []Result{tailscaleServeResult(out)}
		},
	}
}
