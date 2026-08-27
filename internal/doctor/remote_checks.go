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
	for _, specification := range remoteCheckSpecifications(cfg) {
		current := specification.run(ctx, r, cfg.Admin.Username, host, opt)
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
