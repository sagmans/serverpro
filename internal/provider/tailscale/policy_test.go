package tailscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureServerproPolicyAddsTagOwnerAndSSHRule(t *testing.T) {
	var validated, posted map[string]any
	requests := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/acl":
			if r.Header.Get("Accept") != "application/json" {
				t.Fatalf("Accept = %q", r.Header.Get("Accept"))
			}
			w.Header().Set("ETag", `"policy-v1"`)
			_, _ = w.Write([]byte(`{"ssh":[]}`))
		case "POST /tailnet/-/acl/validate":
			if err := json.NewDecoder(r.Body).Decode(&validated); err != nil {
				t.Fatal(err)
			}
		case "POST /tailnet/-/acl":
			if r.Header.Get("If-Match") != `"policy-v1"` {
				t.Fatalf("If-Match = %q", r.Header.Get("If-Match"))
			}
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	change, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).EnsureServerproPolicy(context.Background(), []string{"tag:serverpro-prod"}, "deploy", "check-or-disabled")
	if err != nil {
		t.Fatal(err)
	}
	if !change.SSHRule || strings.Join(change.TagOwners, ",") != "tag:serverpro-prod" {
		t.Fatalf("change = %+v", change)
	}
	for name, body := range map[string]map[string]any{"validated": validated, "posted": posted} {
		tagOwners := body["tagOwners"].(map[string]any)
		owners := tagOwners["tag:serverpro-prod"].([]any)
		if len(owners) != 1 || owners[0] != "autogroup:admin" {
			t.Fatalf("%s tagOwners = %#v", name, tagOwners)
		}
		ssh := body["ssh"].([]any)
		rule := ssh[0].(map[string]any)
		if rule["action"] != "check" || rule["src"].([]any)[0] != "autogroup:admin" || rule["dst"].([]any)[0] != "tag:serverpro-prod" || rule["users"].([]any)[0] != "deploy" {
			t.Fatalf("%s ssh rule = %#v", name, rule)
		}
	}
	if strings.Join(requests, ",") != "GET /tailnet/-/acl,POST /tailnet/-/acl/validate,POST /tailnet/-/acl" {
		t.Fatalf("requests = %v", requests)
	}
}

func TestEnsureServerproPolicyNoopsWhenManagedEntriesExist(t *testing.T) {
	requests := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Path != "/tailnet/-/acl" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tagOwners":{"tag:serverpro-prod":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["deploy"]}]}`))
	}))
	defer ts.Close()

	change, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).EnsureServerproPolicy(context.Background(), []string{"tag:serverpro-prod"}, "deploy", "check-or-disabled")
	if err != nil {
		t.Fatal(err)
	}
	if change.SSHRule || len(change.TagOwners) != 0 {
		t.Fatalf("change = %+v", change)
	}
	if strings.Join(requests, ",") != "GET /tailnet/-/acl" {
		t.Fatalf("requests = %v", requests)
	}
}

func TestRemoveServerproPolicyRemovesOnlyManagedEntries(t *testing.T) {
	var posted map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/acl":
			w.Header().Set("ETag", `"policy-v1"`)
			_, _ = w.Write([]byte(`{"tagOwners":{"tag:serverpro-prod":["autogroup:admin"],"tag:other":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["deploy"]},{"action":"check","src":["autogroup:admin"],"dst":["tag:other"],"users":["deploy"]}]}`))
		case "POST /tailnet/-/acl/validate":
		case "POST /tailnet/-/acl":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	changed, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).RemoveServerproPolicy(context.Background(), []string{"tag:serverpro-prod"}, "deploy", true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected policy change")
	}
	owners := posted["tagOwners"].(map[string]any)
	if _, ok := owners["tag:serverpro-prod"]; ok || owners["tag:other"] == nil {
		t.Fatalf("tagOwners = %#v", owners)
	}
	ssh := posted["ssh"].([]any)
	if len(ssh) != 1 || ssh[0].(map[string]any)["dst"].([]any)[0] != "tag:other" {
		t.Fatalf("ssh = %#v", ssh)
	}
}
