package tailscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateAuthKeyUsesOneOffTaggedServerPolicy(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tailnet/-/keys" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
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

func TestDeleteServerproAuthKeysDeletesBootstrapKeysForTags(t *testing.T) {
	deleted := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/keys":
			_, _ = w.Write([]byte(`{"keys":[{"id":"k1","description":"serverpro bootstrap","capabilities":{"devices":{"create":{"tags":["tag:serverpro-prod"]}}}},{"id":"k2","description":"serverpro bootstrap","capabilities":{"devices":{"create":{"tags":["tag:other"]}}}},{"id":"k3","description":"manual","capabilities":{"devices":{"create":{"tags":["tag:serverpro-prod"]}}}}]}`))
		case "DELETE /tailnet/-/keys/k1":
			deleted = append(deleted, "k1")
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	count, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).DeleteServerproAuthKeys(context.Background(), []string{"tag:serverpro-prod"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(deleted) != 1 || deleted[0] != "k1" {
		t.Fatalf("count=%d deleted=%v", count, deleted)
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
