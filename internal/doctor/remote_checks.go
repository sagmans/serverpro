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
		results = append(results, specification.run(ctx, r, cfg.Admin.Username, host, opt)...)
	}
	return results
}
