package remote

import (
	"context"
	"testing"
	"time"
)

type deadlineRunner struct {
	hasDeadline bool
}

func (r *deadlineRunner) Run(ctx context.Context, _, _, _ string) (string, error) {
	_, r.hasDeadline = ctx.Deadline()
	return "", nil
}

func TestWithTimeoutBoundsAnyRunnerContext(t *testing.T) {
	runner := &deadlineRunner{}
	if _, err := WithTimeout(runner, time.Minute).Run(context.Background(), "user", "host", "true"); err != nil {
		t.Fatal(err)
	}
	if !runner.hasDeadline {
		t.Fatal("non-Tailscale runner received no deadline")
	}
}

func TestContextWithDefaultTimeoutPreservesCallerDeadline(t *testing.T) {
	want := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()
	got, gotCancel := contextWithDefaultTimeout(ctx, 2*time.Minute)
	defer gotCancel()
	deadline, ok := got.Deadline()
	if !ok || !deadline.Equal(want) {
		t.Fatalf("deadline = %v, want %v", deadline, want)
	}
}

func TestContextWithDefaultTimeoutAddsMissingDeadline(t *testing.T) {
	ctx, cancel := contextWithDefaultTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("default timeout did not add a deadline")
	}
}
