package doctor

import (
	"context"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
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
// operator decision (an unknown account, a reboot window). Restricted egress
// has a repair, because config already states the wanted policy.
func remoteDriftSpecifications(cfg config.Config, opt Options) []remoteCheckSpecification {
	user := cfg.Admin.Username
	var specifications []remoteCheckSpecification
	if cfg.Network.Egress.Mode == config.EgressModeRestricted {
		specifications = append(specifications, remoteFixableDriftSpecification(egressRestrictedCheckName, egressRestrictedCommand(), egressRestrictedRemediation, network.LockdownScript(cfg)))
	}
	return append(specifications,
		remoteDriftSpecification(rebootPendingCheckName, rebootPendingCommand(), rebootPendingRemediation),
		remoteDriftSpecification(privilegedAccountsCheckName, privilegedAccountsCommand(user), privilegedAccountsRemediation),
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
	return remoteCheckSpecification{
		readCommands: []string{command},
		run: func(ctx context.Context, runner remote.Runner, user, host string, _ Options) []Result {
			out, err := runner.Run(ctx, user, host, command)
			return []Result{driftResult(name, out, err, remediation)}
		},
	}
}

// remoteFixableDriftSpecification gives a drift check a repair that
// converges the host back to config. The repair runs only on a finding the
// probe could read: a probe that never ran leaves the host state unknown, and
// a repair on an unknown state could undo an operator's deliberate change.
// The check re-reads live after another check ran the same repair, so a
// shared repair settles every check that depends on it.
func remoteFixableDriftSpecification(name, command, remediation, fixCommand string) remoteCheckSpecification {
	return remoteCheckSpecification{
		readCommands:     []string{command},
		liveCommands:     []string{fixCommand},
		refreshAfterLive: []string{fixCommand},
		run: func(ctx context.Context, runner remote.Runner, user, host string, opt Options) []Result {
			if opt.refreshBaseline {
				// The batch baseline predates a repair earlier in this run.
				discardBaseline(runner, command)
			}
			out, err := runner.Run(ctx, user, host, command)
			result := driftResult(name, out, err, remediation)
			if err != nil || result.Status == Pass || !opt.Fix {
				return []Result{result}
			}
			if opt.fixRan != nil && opt.fixRan(fixCommand) {
				return []Result{fail("remote", name, result.Evidence+repairAlreadyRanEvidence, repairAlreadyRanRemediation)}
			}
			if fixOut, fixErr := runner.Run(ctx, user, host, fixCommand); fixErr != nil {
				return []Result{fail("remote", name, result.Evidence+"; fix failed: "+remoteFailureEvidence(fixOut, fixErr), remediation)}
			}
			out, err = runner.Run(ctx, user, host, command)
			after := driftResult(name, out, err, remediation)
			if after.Status == Pass {
				after.Evidence = "fixed: " + after.Evidence
				return []Result{after}
			}
			return []Result{fail("remote", name, "fix applied but drift remains: "+after.Evidence, remediation)}
		},
	}
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
				return []Result{fail("remote", tailscaleServeCheckName, remoteFailureEvidence(out, err), driftProbeFailedRemediation)}
			}
			return []Result{tailscaleServeResult(out)}
		},
	}
}
