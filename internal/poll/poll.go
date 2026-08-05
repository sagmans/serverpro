// Package poll centralizes cancellable wait policy for production polling and deterministic tests.
package poll

import (
	"context"
	"time"
)

// WaitFunc lets tests advance a polling loop without wall-clock sleeps.
type WaitFunc func(context.Context) error

// Wait keeps production timers and deterministic test advancement behind one cancellation contract.
func Wait(ctx context.Context, wait WaitFunc, interval time.Duration) error {
	if wait != nil {
		return wait(ctx)
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
