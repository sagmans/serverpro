package hetzner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestWaitActionReturnsSuccessWithoutSleeping(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"success"}}`))
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", ts.URL, ts.Client()).WaitAction(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	handlerErr.Check()
}

func TestWaitActionUsesDefaultTimeoutWithoutGlobalMutation(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"running"}}`))
	}))
	defer ts.Close()

	sawDeadline := false
	client := NewWithHTTP("token", ts.URL, ts.Client())
	client.wait = func(ctx context.Context) error {
		_, sawDeadline = ctx.Deadline()
		return context.DeadlineExceeded
	}
	err := client.WaitAction(context.Background(), 7)
	handlerErr.Check()
	if !errors.Is(err, context.DeadlineExceeded) || !sawDeadline {
		t.Fatalf("WaitAction() error = %v deadline=%v", err, sawDeadline)
	}
}

func TestWaitActionRetriesRunningStatusWithoutSleeping(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		calls++
		status := "running"
		if calls == 2 {
			status = "success"
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"` + status + `"}}`))
	}))
	defer ts.Close()

	client := NewWithHTTP("token", ts.URL, ts.Client())
	client.wait = func(context.Context) error { return nil }
	err := client.WaitAction(context.Background(), 7)
	handlerErr.Check()
	if err != nil || calls != 2 {
		t.Fatalf("WaitAction() calls=%d error=%v", calls, err)
	}
}

func TestWaitActionReturnsProviderError(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"error","error":{"message":"quota exceeded"}}}`))
	}))
	defer ts.Close()

	err := NewWithHTTP("token", ts.URL, ts.Client()).WaitAction(context.Background(), 7)
	handlerErr.Check()
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("WaitAction() error = %v", err)
	}
}

func TestWaitActionReturnsErrorStatusWithoutProviderMessage(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/7" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":7,"status":"error"}}`))
	}))
	defer ts.Close()

	err := NewWithHTTP("token", ts.URL, ts.Client()).WaitAction(context.Background(), 7)
	handlerErr.Check()
	if err == nil || !strings.Contains(err.Error(), "status=error id=7") {
		t.Fatalf("WaitAction() error = %v", err)
	}
}
