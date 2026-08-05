package tailscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeleteDeviceUsesDeviceEndpoint(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete || r.URL.Path != "/device/n1" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"d1","name":"prod-01.tail.ts.net","hostname":"prod-01","addresses":["100.64.0.1"],"tags":["tag:serverpro-server"],"online":true}]}`))
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dev, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(ctx, DeviceWait{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}})
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID != "d1" {
		t.Fatalf("bad device: %+v", dev)
	}
}

func TestWaitDeviceMatchesConnectedToControlTaggedHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"d1","name":"prod-01.tail.ts.net","hostname":"prod-01","addresses":["100.64.0.1"],"tags":["tag:serverpro-server"],"connectedToControl":true}]}`))
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dev, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(ctx, DeviceWait{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}})
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID != "d1" {
		t.Fatalf("bad device: %+v", dev)
	}
}

func TestMatchingDeviceIDsCapturesEveryStableIdentifier(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"device-old","nodeId":"node-old","hostname":"prod-01","tags":["tag:serverpro-server"]},{"id":"other","nodeId":"node-other","hostname":"other","tags":["tag:serverpro-server"]}]}`))
	}))
	defer ts.Close()
	ids, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).MatchingDeviceIDs(context.Background(), "prod-01", []string{"tag:serverpro-server"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "device-old" || ids[1] != "node-old" {
		t.Fatalf("matching IDs = %v", ids)
	}
}

func TestWaitDeviceExcludesPreexistingDevice(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"device-old","nodeId":"node-old","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true},{"id":"device-new","nodeId":"node-new","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true}]}`))
	}))
	defer ts.Close()
	dev, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(context.Background(), DeviceWait{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}, ExcludedIDs: []string{"device-old", "node-old"}})
	if err != nil {
		t.Fatal(err)
	}
	if dev.NodeID != "node-new" {
		t.Fatalf("selected preexisting device: %+v", dev)
	}
}

func TestWaitDeviceRejectsAmbiguousNewDevices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"device-one","nodeId":"node-one","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true},{"id":"device-two","nodeId":"node-two","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true}]}`))
	}))
	defer ts.Close()
	_, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(context.Background(), DeviceWait{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestWaitDeviceUsesPersistedDeviceID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devices":[{"id":"device-decoy","nodeId":"node-decoy","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true},{"id":"device-bound","nodeId":"node-bound","hostname":"prod-01","tags":["tag:serverpro-server"],"online":true}]}`))
	}))
	defer ts.Close()
	dev, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).WaitDevice(context.Background(), DeviceWait{Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}, DeviceID: "node-bound"})
	if err != nil {
		t.Fatal(err)
	}
	if dev.NodeID != "node-bound" {
		t.Fatalf("persisted binding ignored: %+v", dev)
	}
}

func TestMatchesRequiresAllTags(t *testing.T) {
	device := Device{Name: "prod-01.tail.ts.net", Hostname: "prod-01", Tags: []string{"tag:serverpro-server"}}
	if matches(device, "prod-01", []string{"tag:serverpro-server", "tag:prod"}) {
		t.Fatal("device with missing tag should not match")
	}
}
