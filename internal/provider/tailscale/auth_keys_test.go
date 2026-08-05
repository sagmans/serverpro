package tailscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestCreateAuthKeyUsesOneOffTaggedServerPolicy(t *testing.T) {
	var got map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tailnet/-/keys" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.Record(w, "decode payload: %v", err)
			return
		}
		_, _ = w.Write([]byte(`{"id":"k1","key":"tskey-auth-oneoff"}`))
	}))
	defer ts.Close()
	key, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).CreateAuthKey(context.Background(), []string{"tag:serverpro-server"}, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if key.Key != "tskey-auth-oneoff" {
		t.Fatalf("bad key: %+v", key)
	}
	create := got["capabilities"].(map[string]any)["devices"].(map[string]any)["create"].(map[string]any)
	if create["reusable"].(bool) || create["ephemeral"].(bool) || !create["preauthorized"].(bool) {
		t.Fatalf("unsafe create policy: %+v", create)
	}
	if got["expirySeconds"].(float64) != 1800 {
		t.Fatalf("bad expiry: %+v", got)
	}
}

func TestAuthKeysListsTailnetKeys(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/tailnet/-/keys" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"keys":[{"id":"key-owned","description":"serverpro bootstrap","capabilities":{"devices":{"create":{"tags":["tag:serverpro-prod"]}}}}]}`))
	}))
	defer ts.Close()

	keys, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).AuthKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != "key-owned" || len(keys[0].Capabilities.Devices.Create.Tags) != 1 || keys[0].Capabilities.Devices.Create.Tags[0] != "tag:serverpro-prod" {
		t.Fatalf("keys = %+v", keys)
	}
}

func TestDeleteAuthKeyDeletesTrackedID(t *testing.T) {
	called := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete || r.URL.Path != "/tailnet/-/keys/key-owned" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", "-", ts.URL, ts.Client()).DeleteAuthKey(context.Background(), "key-owned"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("tracked auth key was not deleted")
	}
}

func TestDeleteAuthKeySkipsEmptyID(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer ts.Close()
	if err := NewWithHTTP("token", "-", ts.URL, ts.Client()).DeleteAuthKey(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("empty auth key ID should not call API")
	}
}
