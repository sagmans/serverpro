package doctor

import (
	"context"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
)

func remoteChecksWithOptions(ctx context.Context, cfg config.Config, r remote.Runner, host string, opt Options) []Result {
	if r == nil || host == "" {
		return nil
	}
	if batchRunner, ok := r.(remote.BatchRunner); ok {
		return remoteChecksBatched(ctx, cfg, r, batchRunner, host, opt)
	}
	return remoteChecksSequential(ctx, cfg, r, host, opt)
}

func remoteChecksSequential(ctx context.Context, cfg config.Config, r remote.Runner, host string, opt Options) []Result {
	var results []Result
	tracker := newLiveCommandTracker(r)
	for _, specification := range remoteCheckSpecifications(cfg) {
		specificationOptions := opt
		// A repair earlier in this run can change what a later check reads, and
		// the batch baseline was captured before it ran; such checks re-read live.
		specificationOptions.refreshBaseline = tracker.ranAny(specification.refreshAfterLive)
		specificationOptions.fixRan = tracker.ranScript
		current := specification.run(ctx, tracker, cfg.Admin.Username, host, specificationOptions)
		results = append(results, current...)
		if specification.blocksFixesOnFailure && resultsContainFailure(current) {
			opt.Fix = false
		}
	}
	return results
}

func resultsContainFailure(results []Result) bool {
	for _, result := range results {
		if result.Status == Fail {
			return true
		}
	}
	return false
}
