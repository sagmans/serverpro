package redact

import (
	"errors"
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

func TestRedactorErrorPassesNilThrough(t *testing.T) {
	if New("secret").Error(nil) != nil {
		t.Fatal("nil error should pass through unchanged")
	}
}
