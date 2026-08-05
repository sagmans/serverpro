package hetzner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPowerOnServerReturnsActionID(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/servers/42/actions/poweron" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if r.Body != nil && r.ContentLength > 0 {
			handlerErr.record(w, "expected empty body")
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":78,"status":"running"}}`))
	}))
	defer ts.Close()
	actionID, err := NewWithHTTP("token", ts.URL, ts.Client()).PowerOnServer(context.Background(), 42)
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if actionID != 78 {
		t.Fatalf("action id = %d", actionID)
	}
}

func TestShutdownServerReturnsActionID(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/servers/42/actions/shutdown" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":79,"status":"running"}}`))
	}))
	defer ts.Close()
	actionID, err := NewWithHTTP("token", ts.URL, ts.Client()).ShutdownServer(context.Background(), 42)
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if actionID != 79 {
		t.Fatalf("action id = %d", actionID)
	}
}
