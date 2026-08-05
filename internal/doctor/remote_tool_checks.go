package doctor

import (
	"context"
	"time"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/remote"
)

func remoteToolChecks(ctx context.Context, r remote.Runner, user, host string, opt Options) []Result {
	checks := bootstraptools.Checks(user)
	results := make([]Result, len(checks))
	var failed []int
	for i, check := range checks {
		out, err := r.Run(ctx, user, host, check.Command)
		if err == nil {
			results[i] = pass("remote", check.Name, summarizeRemoteEvidence(check.Name, out))
			continue
		}
		results[i] = fail("remote", check.Name, err.Error(), "run serverpro server doctor --fix")
		failed = append(failed, i)
	}
	if len(failed) == 0 || !opt.Fix {
		return results
	}
	fixRunner := remote.WithTimeout(r, 20*time.Minute)
	if _, err := fixRunner.Run(ctx, user, host, bootstraptools.InstallScriptForUser(user)); err != nil {
		for _, i := range failed {
			results[i] = fail("remote", checks[i].Name, results[i].Evidence+"; fix failed: "+err.Error(), "inspect remote command")
		}
		return results
	}
	fixed := make(map[int]bool, len(failed))
	for _, i := range failed {
		fixed[i] = true
	}
	for i, check := range checks {
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
	return results
}
