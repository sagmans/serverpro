package hetzner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWaitActionReturnsSuccessWithoutSleeping(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"success"}}`))
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", ts.URL, ts.Client()).WaitAction(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	handlerErr.check()
}

func TestWaitActionUsesDefaultTimeout(t *testing.T) {
	oldTimeout := defaultActionTimeout
	defaultActionTimeout = time.Nanosecond
	defer func() { defaultActionTimeout = oldTimeout }()
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"running"}}`))
	}))
	defer ts.Close()

	err := NewWithHTTP("token", ts.URL, ts.Client()).WaitAction(context.Background(), 7)
	handlerErr.check()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitAction() error = %v", err)
	}
}

func TestWaitActionReturnsProviderError(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"error","error":{"message":"quota exceeded"}}}`))
	}))
	defer ts.Close()

	err := NewWithHTTP("token", ts.URL, ts.Client()).WaitAction(context.Background(), 7)
	handlerErr.check()
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("WaitAction() error = %v", err)
	}
}

func TestWaitActionReturnsErrorStatusWithoutProviderMessage(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"error"}}`))
	}))
	defer ts.Close()

	err := NewWithHTTP("token", ts.URL, ts.Client()).WaitAction(context.Background(), 7)
	handlerErr.check()
	if err == nil || !strings.Contains(err.Error(), "status=error id=7") {
		t.Fatalf("WaitAction() error = %v", err)
	}
}
