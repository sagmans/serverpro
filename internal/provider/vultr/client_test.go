package vultr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewUsesVultrBaseURL(t *testing.T) {
	c := New("token")
	if c.api.BaseURL != "https://api.vultr.com/v2" {
		t.Fatalf("base url = %q", c.api.BaseURL)
	}
	if c.api.Token != "token" {
		t.Fatal("token not set")
	}
}

func TestNewWithHTTPOverridesBaseURLAndHTTPClient(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewWithHTTP("token", ts.URL, ts.Client())
	if err := c.api.Do(context.Background(), http.MethodGet, "/regions", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("custom http client not used")
	}
}

func TestClientSendsBearerAuthorization(t *testing.T) {
	var auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	if err := NewWithHTTP("secret-token", ts.URL, ts.Client()).api.Do(context.Background(), http.MethodGet, "/regions", nil, nil); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer secret-token" {
		t.Fatalf("authorization = %q", auth)
	}
}
