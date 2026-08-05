package tailscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDNSConfigFetchesPreferencesAndNameservers(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/tailnet/-/dns/preferences":
			_, _ = w.Write([]byte(`{"magicDNS":true}`))
		case "/tailnet/-/dns/nameservers":
			_, _ = w.Write([]byte(`{"dns":["9.9.9.9","1.1.1.1"],"searchPaths":["tail.ts.net"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).DNSConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MagicDNS {
		t.Fatal("magicDNS = false, want true")
	}
	if len(cfg.GlobalNameservers) != 2 || cfg.GlobalNameservers[0] != "9.9.9.9" {
		t.Fatalf("bad nameservers: %+v", cfg.GlobalNameservers)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 API calls, got %v", paths)
	}
}

func TestDNSConfigPropagatesAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	if _, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).DNSConfig(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
