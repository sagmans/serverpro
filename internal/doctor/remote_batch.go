package doctor

import (
	"context"
	"errors"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
)

var errUnplannedRemoteCommand = errors.New("unplanned remote command")

type remoteReadPlan struct {
	commands     []remote.BatchCommand
	readCommands map[string]struct{}
	liveCommands map[string]struct{}
}

func buildRemoteReadPlan(cfg config.Config) remoteReadPlan {
	var readScripts, liveScripts []string
	for _, specification := range remoteCheckSpecifications(cfg) {
		readScripts = append(readScripts, specification.readCommands...)
		liveScripts = append(liveScripts, specification.liveCommands...)
	}
	commands := make([]remote.BatchCommand, len(readScripts))
	for i, script := range readScripts {
		commands[i] = remote.BatchCommand{Script: script}
	}
	return remoteReadPlan{
		commands:     commands,
		readCommands: commandSet(readScripts...),
		liveCommands: commandSet(liveScripts...),
	}
}

func commandSet(commands ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		set[command] = struct{}{}
	}
	return set
}

func (p remoteReadPlan) hasRead(command string) bool {
	_, ok := p.readCommands[command]
	return ok
}

func (p remoteReadPlan) allowsLive(command string) bool {
	_, ok := p.liveCommands[command]
	return ok
}

func remoteChecksBatched(ctx context.Context, cfg config.Config, source remote.Runner, batch remote.BatchRunner, host string, opt Options) []Result {
	plan := buildRemoteReadPlan(cfg)
	results, err := batch.RunBatch(ctx, cfg.Admin.Username, host, plan.commands)
	if err == nil && len(results) != len(plan.commands) {
		err = fmt.Errorf("remote batch returned %d results; want %d", len(results), len(plan.commands))
	}
	if err == nil {
		for _, result := range results {
			var limitErr *remote.BatchCommandOutputLimitError
			if errors.As(result.Err, &limitErr) {
				// Baseline overflow is untrustworthy evidence, so no fix may run.
				opt.Fix = false
				break
			}
		}
	}
	if err != nil {
		results = make([]remote.BatchResult, len(plan.commands))
		for i := range results {
			results[i].Err = err
		}
		// Fixes require trustworthy baseline evidence; transport/framing failure
		// must not turn a diagnostic run into blind mutation attempts.
		opt.Fix = false
	}
	replay := newBatchReplayRunner(source, plan, results)
	return remoteChecksSequential(ctx, cfg, replay, host, opt)
}

type batchReplayRunner struct {
	source remote.Runner
	plan   remoteReadPlan
	cached map[string][]remote.BatchResult
}

func newBatchReplayRunner(source remote.Runner, plan remoteReadPlan, results []remote.BatchResult) *batchReplayRunner {
	cached := make(map[string][]remote.BatchResult, len(plan.commands))
	for i, command := range plan.commands {
		if i >= len(results) {
			break
		}
		cached[command.Script] = append(cached[command.Script], results[i])
	}
	return &batchReplayRunner{source: source, plan: plan, cached: cached}
}

func (r *batchReplayRunner) Run(ctx context.Context, user, host, script string) (string, error) {
	if results := r.cached[script]; len(results) > 0 {
		result := results[0]
		r.cached[script] = results[1:]
		return result.Output, result.Err
	}
	if !r.plan.hasRead(script) && !r.plan.allowsLive(script) {
		return "", errUnplannedRemoteCommand
	}
	// Planned reads delegate only after their baseline cache is consumed, which
	// limits live calls to explicit fixes and post-fix rechecks.
	return r.source.Run(ctx, user, host, script)
}

func (r *batchReplayRunner) RunWithInput(ctx context.Context, user, host, script, input string) (string, error) {
	if !r.plan.allowsLive(script) {
		return "", errUnplannedRemoteCommand
	}
	inputRunner, ok := r.source.(remote.InputRunner)
	if !ok {
		return "", errors.New("remote runner does not support protected stdin input")
	}
	return inputRunner.RunWithInput(ctx, user, host, script, input)
}
