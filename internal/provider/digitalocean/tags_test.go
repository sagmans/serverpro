package digitalocean

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// WHY: DigitalOcean tags must exist before a droplet references them. EnsureTags
// falls back to GetTag when CreateTag reports a conflict, so a pre-existing tag
// must be treated as success (idempotent create). GetTag was previously 0%.

func TestEnsureTagsTreatsExistingTagAsSuccessViaGetTag(t *testing.T) {
	var mu sync.Mutex
	getChecked := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tags":
			// Simulate "tag already exists" so EnsureTags must confirm via GetTag.
			http.Error(w, `{"id":"conflict"}`, http.StatusConflict)
		case r.Method == http.MethodGet && r.URL.Path == "/tags/serverpro.server:web":
			mu.Lock()
			getChecked = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"tag":{"name":"serverpro.server:web"}}`))
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", ts.URL, ts.Client()).EnsureTags(context.Background(), []string{"serverpro.server:web"}); err != nil {
		t.Fatalf("EnsureTags should tolerate pre-existing tags: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !getChecked {
		t.Fatal("GetTag fallback was not exercised")
	}
}

func TestEnsureTagsFailsWhenTagAbsentAfterCreateError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tags":
			http.Error(w, "boom", http.StatusInternalServerError)
		case r.Method == http.MethodGet && r.URL.Path == "/tags/serverpro.server:web":
			http.Error(w, "missing", http.StatusNotFound)
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", ts.URL, ts.Client()).EnsureTags(context.Background(), []string{"serverpro.server:web"}); err == nil {
		t.Fatal("EnsureTags should surface create error when tag is truly absent")
	}
}
