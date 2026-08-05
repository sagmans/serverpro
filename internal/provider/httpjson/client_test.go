package httpjson

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoRawSendsHeadersAndReturnsResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Accept") != "application/json" || r.Header.Get("If-Match") != `"v1"` {
			t.Fatalf("headers = %#v", r.Header)
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	body, header, err := Client{BaseURL: srv.URL, Token: "token", HTTP: srv.Client()}.DoRaw(context.Background(), http.MethodPost, "/policy", []byte(`{"a":1}`), http.Header{"Accept": {"application/json"}, "If-Match": {`"v1"`}})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"ok":true}` || header.Get("ETag") != `"v2"` {
		t.Fatalf("body/header = %s/%#v", body, header)
	}
}

func TestDoReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	err := Client{BaseURL: srv.URL, HTTP: srv.Client()}.Do(context.Background(), http.MethodGet, "/missing", nil, nil)
	if !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("expected 404 status error, got %v", err)
	}
}
