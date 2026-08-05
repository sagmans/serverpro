package tailscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTailnetIDReturnsMemberTailnetAndIgnoresSharedUsers(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/tailnet/-/users" || r.URL.Query().Get("type") != "member" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"users":[{"type":"member","tailnetId":"tailnet-1"},{"type":"shared","tailnetId":"tailnet-2"},{"type":"member","tailnetId":"tailnet-1"}]}`))
	}))
	defer ts.Close()

	id, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).TailnetID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != "tailnet-1" {
		t.Fatalf("tailnet id = %q", id)
	}
}

func TestTailnetIDRejectsMissingOrInconsistentIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "no users", body: `{"users":[]}`},
		{name: "shared users only", body: `{"users":[{"type":"shared","tailnetId":"tailnet-2"}]}`},
		{name: "missing id", body: `{"users":[{"type":"member"}]}`},
		{name: "mixed member ids", body: `{"users":[{"type":"member","tailnetId":"tailnet-1"},{"type":"member","tailnetId":"tailnet-2"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ts.Close()

			_, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).TailnetID(context.Background())
			if err == nil || !strings.Contains(err.Error(), "tailnet identity") {
				t.Fatalf("expected identity error, got %v", err)
			}
		})
	}
}
