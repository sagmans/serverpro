package tailscale

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestDeleteDeviceUsesDeviceEndpoint(t *testing.T) {
	called := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete || r.URL.Path != "/device/n1" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", "-", ts.URL, ts.Client()).DeleteDevice(context.Background(), "n1"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("delete device endpoint not called")
	}
}

func TestDeleteDeviceSkipsEmptyID(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", "-", ts.URL, ts.Client()).DeleteDevice(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("empty device ID should not call API")
	}
}

func TestWaitDeviceMatchesOnlineTaggedHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"d1","name":"prod-01.tail.ts.net","hostname":"prod-01","addresses":["100.64.0.1"],"tags":["tag:serverpro-server"],"online":true}]}`))
	}))
	defer ts.Close()
	dev, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(context.Background(), mesh.DeviceQuery{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}})
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID != "d1" {
		t.Fatalf("bad device: %+v", dev)
	}
}

func TestWaitDeviceMatchesConnectedToControlTaggedHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"d1","name":"prod-01.tail.ts.net","hostname":"prod-01","addresses":["100.64.0.1"],"tags":["tag:serverpro-server"],"connectedToControl":true}]}`))
	}))
	defer ts.Close()
	dev, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(context.Background(), mesh.DeviceQuery{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}})
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID != "d1" {
		t.Fatalf("bad device: %+v", dev)
	}
}

func TestWaitDeviceRetriesTransientAPIErrorWithoutSleeping(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"devices":[{"id":"d1","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true}]}`))
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	client.wait = func(context.Context) error { return nil }
	dev, err := client.WaitDevice(context.Background(), mesh.DeviceQuery{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}})
	if err != nil || dev.ID != "d1" || calls != 2 {
		t.Fatalf("device=%+v calls=%d error=%v", dev, calls, err)
	}
}

func TestWaitDeviceCancellationIncludesLastAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	client.wait = func(context.Context) error { return context.Canceled }
	_, err := client.WaitDevice(context.Background(), mesh.DeviceQuery{Hostname: "prod-01"})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "last API error") {
		t.Fatalf("error=%v", err)
	}
}

// Ambiguity must stop the wait on the first listing: polling would only give
// an unexpected device more time to look like the managed server.
func TestWaitDeviceRejectsAmbiguousIdentityWithoutPolling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[
			{"id":"d1","nodeId":"n1","name":"prod-01.tail.ts.net","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true,"created":"2026-10-07T10:01:00Z"},
			{"id":"d2","nodeId":"n2","name":"prod-01-1.tail.ts.net","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true,"created":"2026-10-07T10:02:00Z"}
		]}`))
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	client.wait = func(context.Context) error { t.Fatal("ambiguous identity must not poll"); return nil }
	_, err := client.WaitDevice(context.Background(), mesh.DeviceQuery{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}, CreatedNotBefore: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)})
	if !errors.Is(err, mesh.ErrDeviceAmbiguous) {
		t.Fatalf("error=%v", err)
	}
}

// The control-plane "created" field must survive decoding so a stale online
// twin enrolled before the bootstrap key is never chosen.
func TestWaitDeviceSkipsTwinEnrolledBeforeKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[
			{"id":"d1","nodeId":"n-stale","name":"prod-01.tail.ts.net","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true,"created":"2026-10-01T09:00:00Z"},
			{"id":"d2","nodeId":"n-new","name":"prod-01-1.tail.ts.net","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true,"created":"2026-10-07T10:02:00Z"}
		]}`))
	}))
	defer ts.Close()

	dev, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(context.Background(), mesh.DeviceQuery{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}, CreatedNotBefore: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)})
	if err != nil || dev.NodeID != "n-new" {
		t.Fatalf("device=%+v error=%v", dev, err)
	}
}

func TestMatchesRequiresAllTags(t *testing.T) {
	device := Device{Name: "prod-01.tail.ts.net", Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}}
	if mesh.DeviceMatches(device, "prod-01", []string{"tag:serverpro-server", "tag:prod"}) {
		t.Fatal("device with missing tag should not match")
	}
}
