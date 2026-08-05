package redact

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRedactorMasksSecrets(t *testing.T) {
	r := New("tskey-auth-secret", "abc")
	got := r.String("token=tskey-auth-secret short=abc")
	if got != "token=[REDACTED] short=abc" {
		t.Fatalf("got %q", got)
	}
}

func TestRedactorErrorMasksMessage(t *testing.T) {
	r := New("tskey-auth-secret")
	err := r.Error(errors.New("failed: token=tskey-auth-secret"))
	if err.Error() != "failed: token=[REDACTED]" {
		t.Fatalf("got %q", err.Error())
	}
}

func TestRedactorErrorPreservesSentinelIdentity(t *testing.T) {
	err := New("tskey-auth-secret").Error(errors.Join(context.Canceled, errors.New("token=tskey-auth-secret")))
	if !errors.Is(err, context.Canceled) {
		t.Fatal("redaction lost cancellation identity")
	}
	if strings.Contains(err.Error(), "tskey-auth-secret") {
		t.Fatalf("redacted error leaked secret: %q", err)
	}
}

type typedError struct{ message string }

func (e *typedError) Error() string { return e.message }

func TestRedactorErrorPreservesTypedIdentity(t *testing.T) {
	cause := &typedError{message: "token=tskey-auth-secret"}
	err := New("tskey-auth-secret").Error(cause)
	var got *typedError
	if !errors.As(err, &got) || got != cause {
		t.Fatal("redaction lost typed error identity")
	}
	if strings.Contains(err.Error(), "tskey-auth-secret") {
		t.Fatalf("redacted error leaked secret: %q", err)
	}
}

func TestRedactorErrorPassesNilThrough(t *testing.T) {
	if New("secret").Error(nil) != nil {
		t.Fatal("nil error should pass through unchanged")
	}
}
