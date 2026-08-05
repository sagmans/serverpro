package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/assagman/serverpro/internal/provider/httpjson"
)

func TestValidateAccountUsesTunnelListPermission(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/accounts/acc/cfd_tunnel" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer ts.Close()
	if err := NewWithHTTP("token", "acc", ts.URL, ts.Client()).ValidateAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAccountWrapsAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer ts.Close()
	err := NewWithHTTP("token", "acc", ts.URL, ts.Client()).ValidateAccount(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cloudflare account/token validation failed") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestCreateTunnelConnectorOnly(t *testing.T) {
	var got map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/accounts/acc/cfd_tunnel" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"result":{"id":"tun1","name":"prod-01","status":"inactive"}}`))
	}))
	defer ts.Close()
	tun, err := NewWithHTTP("token", "acc", ts.URL, ts.Client()).CreateTunnel(context.Background(), "prod-01")
	if err != nil {
		t.Fatal(err)
	}
	if tun.ID != "tun1" {
		t.Fatalf("bad tunnel: %+v", tun)
	}
	if got["config_src"] != "cloudflare" {
		t.Fatalf("bad payload: %+v", got)
	}
	if _, ok := got["hostname"]; ok {
		t.Fatalf("created hostname route: %+v", got)
	}
}

func TestTunnelHasActiveConnectionsDetectsCloudflare1022(t *testing.T) {
	err := &httpjson.StatusError{
		StatusCode: http.StatusBadRequest,
		Body:       `{"success":false,"errors":[{"code":1022,"message":"This tunnel has active connections."}]}`,
	}
	if !TunnelHasActiveConnections(err) {
		t.Fatal("expected active connection error")
	}
}

func TestTunnelHasActiveConnectionsIgnoresOtherBadRequests(t *testing.T) {
	err := &httpjson.StatusError{
		StatusCode: http.StatusBadRequest,
		Body:       `{"success":false,"errors":[{"code":1003,"message":"other"}]}`,
	}
	if TunnelHasActiveConnections(err) {
		t.Fatal("expected other bad request to be ignored")
	}
}

func TestTunnelHasActiveConnectionsRequiresActiveConnectionMessage(t *testing.T) {
	err := &httpjson.StatusError{
		StatusCode: http.StatusBadRequest,
		Body:       `{"success":false,"errors":[{"code":1022,"message":"other"}]}`,
	}
	if TunnelHasActiveConnections(err) {
		t.Fatal("expected other 1022 error to be ignored")
	}
}

func TestConnectorOnlineReturnsFalseWhenPollDeadlineExpires(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/accounts/acc/cfd_tunnel/tun1" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":{"id":"tun1","name":"prod-01","status":"inactive"}}`))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ok, err := NewWithHTTP("token", "acc", ts.URL, ts.Client()).ConnectorOnline(ctx, "tun1")
	if err != nil || ok {
		t.Fatalf("expected offline without deadline error, ok=%v err=%v", ok, err)
	}
}
