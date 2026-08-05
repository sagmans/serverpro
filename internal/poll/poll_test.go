package poll

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitUsesInjectedPolicy(t *testing.T) {
	want := errors.New("stop")
	called := false
	err := Wait(context.Background(), func(context.Context) error {
		called = true
		return want
	}, time.Hour)
	if !called || !errors.Is(err, want) {
		t.Fatalf("called=%t error=%v", called, err)
	}
}

func TestWaitReturnsContextCancellationWithoutSleeping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Wait(ctx, nil, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestWaitDefaultPolicyCanAdvanceImmediately(t *testing.T) {
	if err := Wait(context.Background(), nil, 0); err != nil {
		t.Fatal(err)
	}
}
