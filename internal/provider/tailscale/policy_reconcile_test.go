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

const reconcilePolicyFixture = `{
	"tagOwners": {
		"tag:serverpro-registered": ["autogroup:admin"],
		"tag:serverpro-live": ["autogroup:admin"],
		"tag:serverpro-stale": ["autogroup:admin"],
		"tag:custom": ["autogroup:admin"]
	},
	"ssh": [
		{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-registered"],"users":["deploy"]},
		{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-live"],"users":["deploy"]},
		{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-stale"],"users":["deploy"]},
		{"action":"check","src":["autogroup:admin"],"dst":["tag:custom"],"users":["deploy"]}
	]
}`

const reconcileOwnerDependencyPolicyFixture = `{
	"tagOwners": {
		"tag:serverpro-active": ["autogroup:admin", "tag:serverpro-automation"],
		"tag:serverpro-automation": ["autogroup:admin"],
		"tag:serverpro-stale": ["autogroup:admin"]
	},
	"ssh": [
		{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-stale"],"users":["deploy"]}
	]
}`

func TestPlanServerproPolicyReconcileUsesRegisteredAndLiveEvidence(t *testing.T) {
	posts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[{"id":"live","tags":["tag:serverpro-live"]}]}`))
		case "GET /tailnet/-/acl":
			_, _ = w.Write([]byte(reconcilePolicyFixture))
		default:
			posts++
		}
	}))
	defer ts.Close()

	plan, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).PlanServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-registered"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.TagOwners, ",") != "tag:serverpro-stale" || len(plan.SSHRules) != 1 || plan.SSHRules[0].Dst[0] != "tag:serverpro-stale" {
		t.Fatalf("plan=%+v", plan)
	}
	if posts != 0 {
		t.Fatalf("plan mutated policy with %d POST requests", posts)
	}
}

func TestApplyServerproPolicyReconcilePreservesTagsReferencedByRetainedOwners(t *testing.T) {
	var posted map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case "GET /tailnet/-/acl":
			w.Header().Set("ETag", `"policy-v1"`)
			_, _ = w.Write([]byte(reconcileOwnerDependencyPolicyFixture))
		case "POST /tailnet/-/acl/validate":
		case "POST /tailnet/-/acl":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				handlerErr.Record(w, "decode policy: %v", err)
			}
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	plan, err := client.PlanServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-active"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.TagOwners, ",") != "tag:serverpro-stale" || len(plan.SSHRules) != 1 || plan.SSHRules[0].Dst[0] != "tag:serverpro-stale" {
		t.Fatalf("plan=%+v", plan)
	}
	if err := client.ApplyServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-active"}, plan); err != nil {
		t.Fatal(err)
	}
	owners := posted["tagOwners"].(map[string]any)
	if owners["tag:serverpro-active"] == nil || owners["tag:serverpro-automation"] == nil || owners["tag:serverpro-stale"] != nil {
		t.Fatalf("tagOwners=%+v", owners)
	}
}

func TestReconcileServerproPolicySkipsMutationWhenNothingIsUnused(t *testing.T) {
	requests := []string{}
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case "GET /tailnet/-/acl":
			_, _ = w.Write([]byte(`{"tagOwners":{"tag:serverpro-used":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-used"],"users":["deploy"]}]}`))
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	approved := ServerproPolicyReconcilePlan{}
	if err := client.ApplyServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-used"}, approved); err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, ",") != "GET /tailnet/-/devices,GET /tailnet/-/acl" {
		t.Fatalf("requests=%v", requests)
	}
}

func TestPlanServerproPolicyReconcilePlansRecognizedMixedDestinationsAndSortsOwners(t *testing.T) {
	policy := `{
		"tagOwners": {
			"tag:serverpro-z": ["autogroup:admin"],
			"tag:serverpro-mixed": ["autogroup:admin", "user:owner@example.com"],
			"tag:serverpro-a": ["autogroup:admin"]
		},
		"ssh": [
			{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-a","tag:serverpro-mixed"],"users":["deploy"]},
			{"action":"accept","src":["autogroup:admin"],"dst":["tag:serverpro-a"],"users":["deploy"]},
			{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-z"],"users":["deploy","root"]}
		]
	}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tailnet/-/devices" {
			_, _ = w.Write([]byte(`{"devices":[]}`))
			return
		}
		_, _ = w.Write([]byte(policy))
	}))
	defer ts.Close()

	plan, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).PlanServerproPolicyReconcile(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.TagOwners, ",") != "tag:serverpro-a,tag:serverpro-z" || len(plan.SSHRules) != 1 || strings.Join(plan.SSHRules[0].Dst, ",") != "tag:serverpro-a" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestApplyServerproPolicyReconcileRejectsChangedPlan(t *testing.T) {
	policyCalls := 0
	posts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case "GET /tailnet/-/acl":
			policyCalls++
			if policyCalls == 1 {
				_, _ = w.Write([]byte(`{"tagOwners":{"tag:serverpro-stale":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-stale"],"users":["deploy"]}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"tagOwners":{"tag:serverpro-stale":["autogroup:admin"],"tag:serverpro-new":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-stale"],"users":["deploy"]},{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-new"],"users":["deploy"]}]}`))
		default:
			posts++
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	approved, err := client.PlanServerproPolicyReconcile(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyServerproPolicyReconcile(context.Background(), nil, approved); !errors.Is(err, ErrPolicyReconcilePlanChanged) {
		t.Fatalf("changed plan error=%v", err)
	}
	if posts != 0 {
		t.Fatalf("changed plan made %d POST requests", posts)
	}
}

func TestApplyServerproPolicyReconcileRewritesMixedDestinations(t *testing.T) {
	policy := `{"tagOwners":{"tag:serverpro-stale":["autogroup:admin"],"tag:serverpro-used":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-stale","tag:serverpro-used"],"users":["deploy"],"checkPeriod":"12h","acceptEnv":["GIT_*"],"future":{"enabled":true}}]}`
	var validated, posted map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case "GET /tailnet/-/acl":
			w.Header().Set("ETag", `"policy-v1"`)
			_, _ = w.Write([]byte(policy))
		case "POST /tailnet/-/acl/validate":
			if err := json.NewDecoder(r.Body).Decode(&validated); err != nil {
				handlerErr.Record(w, "decode validation policy: %v", err)
			}
		case "POST /tailnet/-/acl":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				handlerErr.Record(w, "decode policy: %v", err)
			}
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	approved, err := client.PlanServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-used"})
	if err != nil {
		t.Fatal(err)
	}
	if len(approved.SSHRules) != 1 || strings.Join(approved.SSHRules[0].Dst, ",") != "tag:serverpro-stale" {
		t.Fatalf("approved=%+v", approved)
	}
	if err := client.ApplyServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-used"}, approved); err != nil {
		t.Fatal(err)
	}
	owners := posted["tagOwners"].(map[string]any)
	if owners["tag:serverpro-stale"] != nil || owners["tag:serverpro-used"] == nil {
		t.Fatalf("posted=%+v", posted)
	}
	for name, body := range map[string]map[string]any{"validated": validated, "posted": posted} {
		rules := body["ssh"].([]any)
		if len(rules) != 1 {
			t.Fatalf("%s policy rules=%+v", name, rules)
		}
		rule := rules[0].(map[string]any)
		dst := rule["dst"].([]any)
		acceptEnv, _ := rule["acceptEnv"].([]any)
		future, _ := rule["future"].(map[string]any)
		if len(dst) != 1 || dst[0] != "tag:serverpro-used" || rule["checkPeriod"] != "12h" || len(acceptEnv) != 1 || future["enabled"] != true {
			t.Fatalf("%s mixed rule lost fields: %+v", name, rule)
		}
	}
}

func TestApplyServerproPolicyReconcileDoesNotPublishInvalidRewrite(t *testing.T) {
	const policy = `{"tagOwners":{"tag:serverpro-stale":["autogroup:admin"],"tag:serverpro-used":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-stale","tag:serverpro-used"],"users":["deploy"]}]}`
	validationPosts := 0
	policyPosts := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case "GET /tailnet/-/acl":
			w.Header().Set("ETag", `"policy-v1"`)
			_, _ = w.Write([]byte(policy))
		case "POST /tailnet/-/acl/validate":
			validationPosts++
			http.Error(w, "invalid policy", http.StatusUnprocessableEntity)
		case "POST /tailnet/-/acl":
			policyPosts++
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	approved, err := client.PlanServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-used"})
	if err != nil {
		t.Fatal(err)
	}
	err = client.ApplyServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-used"}, approved)
	if err == nil || validationPosts != 1 || policyPosts != 0 {
		t.Fatalf("err=%v validationPosts=%d policyPosts=%d", err, validationPosts, policyPosts)
	}
}

func TestReconcileServerproPolicyRemovesOnlyProvenUnusedEntries(t *testing.T) {
	var posted map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[{"id":"live","tags":["tag:serverpro-live"]}]}`))
		case "GET /tailnet/-/acl":
			w.Header().Set("ETag", `"policy-v1"`)
			_, _ = w.Write([]byte(reconcilePolicyFixture))
		case "POST /tailnet/-/acl/validate":
		case "POST /tailnet/-/acl":
			if r.Header.Get("If-Match") != `"policy-v1"` {
				handlerErr.Record(w, "If-Match = %q", r.Header.Get("If-Match"))
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				handlerErr.Record(w, "decode policy: %v", err)
				return
			}
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	plan, err := client.PlanServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-registered"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.TagOwners, ",") != "tag:serverpro-stale" || len(plan.SSHRules) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	if err := client.ApplyServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-registered"}, plan); err != nil {
		t.Fatal(err)
	}
	owners := posted["tagOwners"].(map[string]any)
	if owners["tag:serverpro-stale"] != nil || owners["tag:serverpro-registered"] == nil || owners["tag:serverpro-live"] == nil || owners["tag:custom"] == nil {
		t.Fatalf("tagOwners=%+v", owners)
	}
	for _, raw := range posted["ssh"].([]any) {
		dst := raw.(map[string]any)["dst"].([]any)[0]
		if dst == "tag:serverpro-stale" {
			t.Fatalf("stale SSH rule survived: %+v", posted["ssh"])
		}
	}
}

func TestApplyServerproPolicyReconcileRejectsMutationWithoutETag(t *testing.T) {
	policyPosts := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/devices":
			_, _ = w.Write([]byte(`{"devices":[]}`))
		case "GET /tailnet/-/acl":
			_, _ = w.Write([]byte(`{"tagOwners":{"tag:serverpro-stale":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-stale"],"users":["deploy"]}]}`))
		case "POST /tailnet/-/acl/validate":
		case "POST /tailnet/-/acl":
			policyPosts++
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", "-", ts.URL, ts.Client())
	approved, err := client.PlanServerproPolicyReconcile(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = client.ApplyServerproPolicyReconcile(context.Background(), nil, approved)
	if !errors.Is(err, ErrPolicyETagMissing) || policyPosts != 0 {
		t.Fatalf("err=%v policyPosts=%d", err, policyPosts)
	}
}
