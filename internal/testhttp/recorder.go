// Package testhttp provides concurrency-safe HTTP handler assertions for tests.
package testhttp

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
)

const unexpectedRequestMessage = "unexpected test request"

// Recorder defers handler failures to the test goroutine, where testing assertions are legal.
type Recorder struct {
	mu     sync.Mutex
	errors []error
}

// HandlerErrorRecorder retains the owning test so package tests need no local adapters.
type HandlerErrorRecorder struct {
	t testing.TB
	Recorder
}

// NewHandlerErrorRecorder binds deferred handler failures to their owning test.
func NewHandlerErrorRecorder(t testing.TB) *HandlerErrorRecorder {
	t.Helper()
	return &HandlerErrorRecorder{t: t}
}

// Record captures a handler failure without calling testing methods from the server goroutine.
func (r *Recorder) Record(w http.ResponseWriter, format string, args ...any) {
	r.mu.Lock()
	r.errors = append(r.errors, fmt.Errorf(format, args...))
	r.mu.Unlock()
	http.Error(w, unexpectedRequestMessage, http.StatusBadRequest)
}

// Err joins every recorded failure so concurrent requests cannot hide earlier evidence.
func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.errors...)
}

// Check reports recorded failures from the owning test goroutine.
func (r *Recorder) Check(t testing.TB) {
	t.Helper()
	if err := r.Err(); err != nil {
		t.Error(err)
	}
}

// Check reports handler failures through the recorder's owning test.
func (r *HandlerErrorRecorder) Check() {
	r.Recorder.Check(r.t)
}
