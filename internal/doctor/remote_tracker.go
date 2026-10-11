package doctor

import (
	"context"
	"errors"

	"github.com/sagmans/serverpro/internal/remote"
)

// baselineDiscarder drops the cached batch baseline for one read, so the next
// Run of that script goes to the host. Runners without a baseline already
// read live and need not implement it.
type baselineDiscarder interface {
	discardBaseline(script string)
}

// liveCommandTracker records which scripts a doctor run sent, so checks that
// depend on a repair (refreshAfterLive) know that their baseline is stale.
type liveCommandTracker struct {
	runner remote.Runner
	ran    map[string]bool
}

func newLiveCommandTracker(r remote.Runner) *liveCommandTracker {
	return &liveCommandTracker{runner: r, ran: map[string]bool{}}
}

func (t *liveCommandTracker) Run(ctx context.Context, user, host, script string) (string, error) {
	t.ran[script] = true
	return t.runner.Run(ctx, user, host, script)
}

func (t *liveCommandTracker) RunWithInput(ctx context.Context, user, host, script, input string) (string, error) {
	inputRunner, ok := t.runner.(remote.InputRunner)
	if !ok {
		return "", errors.New("remote runner does not support protected stdin input")
	}
	t.ran[script] = true
	return inputRunner.RunWithInput(ctx, user, host, script, input)
}

func (t *liveCommandTracker) discardBaseline(script string) {
	if discarder, ok := t.runner.(baselineDiscarder); ok {
		discarder.discardBaseline(script)
	}
}

func (t *liveCommandTracker) ranAny(scripts []string) bool {
	for _, script := range scripts {
		if t.ran[script] {
			return true
		}
	}
	return false
}

func discardBaseline(r remote.Runner, script string) {
	if discarder, ok := r.(baselineDiscarder); ok {
		discarder.discardBaseline(script)
	}
}
