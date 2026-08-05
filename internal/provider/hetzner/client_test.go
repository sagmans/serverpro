package hetzner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestCreateServerAttachesFirewallAndCloudInit(t *testing.T) {
	var got map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/servers" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			handlerErr.Record(w, "missing auth header")
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.Record(w, "decode request: %v", err)
			return
		}
		_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-01","labels":{"managed-by":"serverpro"},"public_net":{"ipv4":{"ip":"203.0.113.10"}}},"action":{"id":7,"status":"running"}}`))
	}))
	defer ts.Close()
	input := CreateServerInput{Name: "prod-01", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"}
	server, action, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateServer(context.Background(), input, 99, "#cloud-config")
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if server.ID != 42 || action != 7 {
		t.Fatalf("bad response: %+v %d", server, action)
	}
	if got["user_data"] != "#cloud-config" {
		t.Fatalf("missing user_data: %+v", got)
	}
	fws := got["firewalls"].([]any)
	fw := fws[0].(map[string]any)
	if fw["firewall"].(float64) != 99 {
		t.Fatalf("firewall not attached: %+v", got)
	}
}

func TestDeleteServerReturnsActionID(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/servers/42" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"action":{"id":77,"status":"running"}}`))
	}))
	defer ts.Close()
	actionID, err := NewWithHTTP("token", ts.URL, ts.Client()).DeleteServer(context.Background(), 42)
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if actionID != 77 {
		t.Fatalf("action id = %d", actionID)
	}
}

func TestGetServerDecodesStatus(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/servers/42" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-01","status":"off","labels":{"managed-by":"serverpro"}}}`))
	}))
	defer ts.Close()
	server, err := NewWithHTTP("token", ts.URL, ts.Client()).GetServer(context.Background(), 42)
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if server.Status != "off" {
		t.Fatalf("status = %q", server.Status)
	}
}
