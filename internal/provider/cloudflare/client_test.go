package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestListTunnelsPagesAllResults(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acc/cfd_tunnel" {
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
			return
		}
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = w.Write([]byte(`{"result":[{"id":"t1","name":"a"}],"result_info":{"page":1,"total_pages":2}}`))
		case "2":
			_, _ = w.Write([]byte(`{"result":[{"id":"t2","name":"b"}],"result_info":{"page":2,"total_pages":2}}`))
		default:
			handlerErr.Record(w, "unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer ts.Close()
	tunnels, err := NewWithHTTP("token", "acc", ts.URL, ts.Client()).ListTunnels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tunnels) != 2 || tunnels[0].ID != "t1" || tunnels[1].ID != "t2" {
		t.Fatalf("tunnels = %+v", tunnels)
	}
}

func TestMatchTunnelByNameUsesExactUniqueImportSemantics(t *testing.T) {
	tunnels := []Tunnel{{ID: "one", Name: "prod-web"}, {ID: "other", Name: "other"}}
	got, found, err := MatchTunnelByName(tunnels, "prod-web")
	if err != nil || !found || got.ID != "one" {
		t.Fatalf("unique match = %+v found=%t err=%v", got, found, err)
	}
	if got, found, err = MatchTunnelByName(tunnels, "missing"); err != nil || found || got.ID != "" {
		t.Fatalf("missing match = %+v found=%t err=%v", got, found, err)
	}
	_, found, err = MatchTunnelByName([]Tunnel{{ID: "one", Name: "prod-web"}, {ID: "two", Name: "prod-web"}}, "prod-web")
	if err == nil || found || !strings.Contains(err.Error(), `cloudflare tunnel "prod-web" is ambiguous`) {
		t.Fatalf("ambiguous match found=%t err=%v", found, err)
	}
}

func TestTunnelTokenReturnsConnectorToken(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/accounts/acc/cfd_tunnel/tun1/token" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"result":"connector-token"}`))
	}))
	defer ts.Close()
	token, err := NewWithHTTP("token", "acc", ts.URL, ts.Client()).TunnelToken(context.Background(), "tun1")
	if err != nil || token != "connector-token" {
		t.Fatalf("token = %q err = %v", token, err)
	}
}

func TestDeleteTunnelSkipsEmptyIDAndCallsAPIOtherwise(t *testing.T) {
	called := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete || r.URL.Path != "/accounts/acc/cfd_tunnel/tun1" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	client := NewWithHTTP("token", "acc", ts.URL, ts.Client())
	if err := client.DeleteTunnel(context.Background(), ""); err != nil {
		t.Fatalf("empty id should be a no-op: %v", err)
	}
	if called {
		t.Fatal("empty tunnel id must not call the API")
	}
	if err := client.DeleteTunnel(context.Background(), "tun1"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("delete endpoint not called for non-empty id")
	}
}

func TestNewBuildsAccountScopedProductionClient(t *testing.T) {
	// WHY: the production constructor pins the public API base + account scoping.
	if got := New("token", "acc").path("/cfd_tunnel"); got != "/accounts/acc/cfd_tunnel" {
		t.Fatalf("account scoping wrong: %q", got)
	}
}

func TestValidateAccountUsesTunnelListPermission(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/accounts/acc/cfd_tunnel" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
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
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/accounts/acc/cfd_tunnel" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.Record(w, "decode payload: %v", err)
			return
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

func TestConnectorOnlineRetriesInactiveTunnelWithoutSleeping(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		status := "inactive"
		if calls == 2 {
			status = "healthy"
		}
		_, _ = w.Write([]byte(`{"result":{"id":"tun1","status":"` + status + `"}}`))
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "acc", ts.URL, ts.Client())
	client.wait = func(context.Context) error { return nil }
	ok, err := client.ConnectorOnline(context.Background(), "tun1")
	if err != nil || !ok || calls != 2 {
		t.Fatalf("online=%v calls=%d error=%v", ok, calls, err)
	}
}

func TestConnectorOnlineReturnsFalseWhenPollDeadlineExpires(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"id":"tun1","status":"inactive"}}`))
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "acc", ts.URL, ts.Client())
	client.wait = func(context.Context) error { return context.DeadlineExceeded }
	ok, err := client.ConnectorOnline(context.Background(), "tun1")
	if err != nil || ok {
		t.Fatalf("expected offline without deadline error, ok=%v err=%v", ok, err)
	}
}

func TestConnectorOnlineReturnsPollCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"id":"tun1","status":"inactive"}}`))
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "acc", ts.URL, ts.Client())
	client.wait = func(context.Context) error { return context.Canceled }
	ok, err := client.ConnectorOnline(context.Background(), "tun1")
	if ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("online=%v error=%v", ok, err)
	}
}

func TestConnectorOnlineReturnsTerminalAPIErrorWithoutPolling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer ts.Close()

	waited := false
	client := NewWithHTTP("token", "acc", ts.URL, ts.Client())
	client.wait = func(context.Context) error {
		waited = true
		return nil
	}
	if _, err := client.ConnectorOnline(context.Background(), "tun1"); err == nil || waited {
		t.Fatalf("error=%v waited=%v", err, waited)
	}
}
