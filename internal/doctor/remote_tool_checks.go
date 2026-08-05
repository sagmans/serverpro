package doctor

import (
	"context"
	"slices"
	"time"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/poll"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/tailscaletools"
)

const (
	tailscaleRecheckAttempts = 6
	tailscaleRecheckInterval = 2 * time.Second
)

type remoteToolRepair uint8

const (
	remoteToolRepairBootstrap remoteToolRepair = iota
	remoteToolRepairTailscale
)

type remoteToolDefinition struct {
	bootstraptools.Check
	repair remoteToolRepair
}

func remoteToolDefinitions(user string) []remoteToolDefinition {
	checks := bootstraptools.Checks(user)
	definitions := make([]remoteToolDefinition, 0, len(checks)+1)
	for _, check := range checks {
		definitions = append(definitions, remoteToolDefinition{Check: check, repair: remoteToolRepairBootstrap})
	}
	definitions = append(definitions, remoteToolDefinition{
		Check:  bootstraptools.Check{Name: tailscaletools.CheckName, Command: tailscaletools.CheckCommand()},
		repair: remoteToolRepairTailscale,
	})
	return definitions
}

func remoteToolChecks(ctx context.Context, r remote.Runner, user, host string, opt Options) []Result {
	return remoteToolChecksWithWait(ctx, r, user, host, opt, nil)
}

func remoteToolChecksWithWait(ctx context.Context, r remote.Runner, user, host string, opt Options, wait poll.WaitFunc) []Result {
	checks := remoteToolDefinitions(user)
	results := make([]Result, len(checks))
	failed := make(map[remoteToolRepair][]int)
	for i, check := range checks {
		out, err := r.Run(ctx, user, host, check.Command)
		if err == nil {
			results[i] = pass("remote", check.Name, summarizeRemoteEvidence(check.Name, out))
			continue
		}
		results[i] = fail("remote", check.Name, err.Error(), "run serverpro server doctor --fix")
		failed[check.repair] = append(failed[check.repair], i)
	}
	if !opt.Fix {
		return results
	}

	fixRunner := remote.WithTimeout(r, 20*time.Minute)
	// Refresh metadata before trusting an initially-clean package simulation.
	// Existing package failures already enter bootstrap, which refreshes there.
	for i, check := range checks {
		if check.Name != bootstraptools.ManagedPackageCheckName || slices.Contains(failed[remoteToolRepairBootstrap], i) {
			continue
		}
		if _, err := fixRunner.Run(ctx, user, host, bootstraptools.ManagedPackageRefreshCommand()); err != nil {
			results[i] = fail("remote", check.Name, results[i].Evidence+"; fix failed: "+err.Error(), "inspect remote command")
			break
		}
		out, err := fixRunner.Run(ctx, user, host, check.Command)
		if err != nil {
			results[i] = fail("remote", check.Name, err.Error(), "run serverpro server doctor --fix")
			failed[remoteToolRepairBootstrap] = append(failed[remoteToolRepairBootstrap], i)
		} else {
			results[i] = pass("remote", check.Name, "fixed: "+summarizeRemoteEvidence(check.Name, out))
		}
		break
	}
	if len(failed) == 0 {
		return results
	}
	if indices := failed[remoteToolRepairBootstrap]; len(indices) > 0 {
		repairBootstrapTools(ctx, fixRunner, user, host, checks, indices, results)
	}
	if indices := failed[remoteToolRepairTailscale]; len(indices) > 0 {
		repairTailscale(ctx, fixRunner, user, host, checks, indices, results, wait)
	}
	return results
}

func repairBootstrapTools(ctx context.Context, r remote.Runner, user, host string, checks []remoteToolDefinition, failed []int, results []Result) {
	if _, err := r.Run(ctx, user, host, bootstraptools.InstallScriptForUser(user)); err != nil {
		markToolFixFailed(checks, failed, results, err.Error())
		return
	}
	fixed := indexSet(failed)
	for i, check := range checks {
		if check.repair != remoteToolRepairBootstrap {
			continue
		}
		out, err := r.Run(ctx, user, host, check.Command)
		if err != nil {
			results[i] = fail("remote", check.Name, err.Error(), "fix applied but check still failed")
			continue
		}
		evidence := summarizeRemoteEvidence(check.Name, out)
		if fixed[i] {
			evidence = "fixed: " + evidence
		}
		results[i] = pass("remote", check.Name, evidence)
	}
}

func repairTailscale(ctx context.Context, r remote.Runner, user, host string, checks []remoteToolDefinition, failed []int, results []Result, wait poll.WaitFunc) {
	if _, err := r.Run(ctx, user, host, tailscaletools.UpdateScript()); err != nil {
		markToolFixFailed(checks, failed, results, err.Error())
		return
	}
	if err := poll.Wait(ctx, wait, tailscaletools.RestartGrace); err != nil {
		markToolFixFailed(checks, failed, results, err.Error())
		return
	}

	for _, i := range failed {
		check := checks[i]
		var lastErr error
		for attempt := 0; attempt < tailscaleRecheckAttempts; attempt++ {
			out, err := r.Run(ctx, user, host, check.Command)
			if err == nil {
				results[i] = pass("remote", check.Name, "fixed: "+summarizeRemoteEvidence(check.Name, out))
				lastErr = nil
				break
			}
			lastErr = err
			if attempt+1 < tailscaleRecheckAttempts {
				if err := poll.Wait(ctx, wait, tailscaleRecheckInterval); err != nil {
					lastErr = err
					break
				}
			}
		}
		if lastErr != nil {
			results[i] = fail("remote", check.Name, lastErr.Error(), "fix applied but check still failed")
		}
	}
}

func markToolFixFailed(checks []remoteToolDefinition, failed []int, results []Result, evidence string) {
	for _, i := range failed {
		results[i] = fail("remote", checks[i].Name, results[i].Evidence+"; fix failed: "+evidence, "inspect remote command")
	}
}

func indexSet(indices []int) map[int]bool {
	set := make(map[int]bool, len(indices))
	for _, index := range indices {
		set[index] = true
	}
	return set
}
