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

// newLiveCommandTracker wraps one doctor run's runner; its record lives only
// for that run, so a fresh doctor run may repair again.
func newLiveCommandTracker(r remote.Runner) *liveCommandTracker {
	return &liveCommandTracker{runner: r, ran: map[string]bool{}}
}

// Run records the script before sending it, so a repair that times out or
// fails still counts as tried.
func (t *liveCommandTracker) Run(ctx context.Context, user, host, script string) (string, error) {
	t.ran[script] = true
	return t.runner.Run(ctx, user, host, script)
}

// RunWithInput records like Run; the input never enters the record, because
// it can carry credentials.
func (t *liveCommandTracker) RunWithInput(ctx context.Context, user, host, script, input string) (string, error) {
	inputRunner, ok := t.runner.(remote.InputRunner)
	if !ok {
		return "", errors.New("remote runner does not support protected stdin input")
	}
	t.ran[script] = true
	return inputRunner.RunWithInput(ctx, user, host, script, input)
}

// discardBaseline forwards to the wrapped runner, so the tracker stays
// transparent to remoteFixableCheck.
func (t *liveCommandTracker) discardBaseline(script string) {
	if discarder, ok := t.runner.(baselineDiscarder); ok {
		discarder.discardBaseline(script)
	}
}

// ranScript backs Options.fixRan. Doctor runs its checks one after another,
// so the record needs no lock.
func (t *liveCommandTracker) ranScript(script string) bool {
	return t.ran[script]
}

// ranAny backs refreshAfterLive: any listed repair makes the baseline stale.
func (t *liveCommandTracker) ranAny(scripts []string) bool {
	for _, script := range scripts {
		if t.ranScript(script) {
			return true
		}
	}
	return false
}

// discardBaseline is a no-op for runners that read live already. The
// cloudflared check wraps the tracker in remote.WithTimeout, which hides this
// method, so a refreshAfterLive on that check would need the wrapper to
// forward it first.
func discardBaseline(r remote.Runner, script string) {
	if discarder, ok := r.(baselineDiscarder); ok {
		discarder.discardBaseline(script)
	}
}
