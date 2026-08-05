package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestEnsureServerproPolicyAddsTagOwnerAndSSHRule(t *testing.T) {
	var validated, posted map[string]any
	requests := []string{}
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/acl":
			if r.Header.Get("Accept") != "application/json" {
				handlerErr.Record(w, "Accept = %q", r.Header.Get("Accept"))
				return
			}
			w.Header().Set("ETag", `"policy-v1"`)
			_, _ = w.Write([]byte(`{"ssh":[]}`))
		case "POST /tailnet/-/acl/validate":
			if err := json.NewDecoder(r.Body).Decode(&validated); err != nil {
				handlerErr.Record(w, "decode validation payload: %v", err)
				return
			}
		case "POST /tailnet/-/acl":
			if r.Header.Get("If-Match") != `"policy-v1"` {
				handlerErr.Record(w, "If-Match = %q", r.Header.Get("If-Match"))
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				handlerErr.Record(w, "decode policy payload: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
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

func TestEnsureServerproPolicyRejectsMutationWithoutETag(t *testing.T) {
	policyPosts := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/acl":
			_, _ = w.Write([]byte(`{"ssh":[]}`))
		case "POST /tailnet/-/acl/validate":
		case "POST /tailnet/-/acl":
			policyPosts++
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	_, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).EnsureServerproPolicy(context.Background(), []string{"tag:serverpro-prod"}, "deploy", "check-or-disabled")
	if !errors.Is(err, ErrPolicyETagMissing) || policyPosts != 0 {
		t.Fatalf("err=%v policyPosts=%d", err, policyPosts)
	}
}

func TestEnsureServerproPolicyNoopsWhenManagedEntriesExist(t *testing.T) {
	requests := []string{}
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Path != "/tailnet/-/acl" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
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
