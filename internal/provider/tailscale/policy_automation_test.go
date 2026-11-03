package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/testhttp"
)

const (
	automationTestTag   = "tag:serverpro-prod"
	automationTestAdmin = "deploy"
	automationTestETag  = `"policy-v1"`
	automationTestRoot  = "check-or-disabled"
)

// automationPolicyAPI keeps one policy document in memory, so a test can
// assert both the document serverpro posts and the request sequence. The
// policy file is the only shared tailnet object, so the exact write matters.
type automationPolicyAPI struct {
	mu       sync.Mutex
	doc      string
	posted   map[string]any
	requests []string
}

func newAutomationPolicyAPI(t *testing.T, doc string) (*automationPolicyAPI, Client) {
	t.Helper()
	api := &automationPolicyAPI{doc: doc}
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	t.Cleanup(handlerErr.Check)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		api.requests = append(api.requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/acl":
			w.Header().Set("ETag", automationTestETag)
			_, _ = io.WriteString(w, api.doc)
		case "POST /tailnet/-/acl/validate":
		case "POST /tailnet/-/acl":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				handlerErr.Record(w, "read policy payload: %v", err)
				return
			}
			api.posted = map[string]any{}
			if err := json.Unmarshal(body, &api.posted); err != nil {
				handlerErr.Record(w, "decode policy payload: %v", err)
				return
			}
			api.doc = string(body)
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return api, NewWithHTTP("token", "-", srv.URL, srv.Client())
}

func (a *automationPolicyAPI) postedSSH(t *testing.T) []mesh.SSHRule {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.posted == nil {
		t.Fatal("no policy was posted")
	}
	raw, err := json.Marshal(a.posted["ssh"])
	if err != nil {
		t.Fatal(err)
	}
	var rules []mesh.SSHRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

func countRules(rules []mesh.SSHRule, action, src string) int {
	n := 0
	for _, rule := range rules {
		if rule.Action == action && len(rule.Src) == 1 && rule.Src[0] == src {
			n++
		}
	}
	return n
}

// The accept rule is the one place serverpro lets SSH skip step-up auth, so
// its exact shape (admin user only, the fixed tag only, server tags only) is
// the security boundary under test, together with the tag owner that makes
// the automation tag assignable at all.
func TestEnsureServerproPolicyAddsAutomationAcceptRule(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, `{"ssh":[]}`)

	change, err := client.EnsureServerproPolicy(context.Background(), []string{automationTestTag}, automationTestAdmin, automationTestRoot, mesh.AutomationSourceTag)
	if err != nil {
		t.Fatal(err)
	}
	if !change.SSHRule || !change.AutomationSSHRule || change.AutomationSSHRuleRemoved {
		t.Fatalf("change = %+v", change)
	}
	if owners := slices.Sorted(slices.Values(change.TagOwners)); !slices.Equal(owners, []string{mesh.AutomationSourceTag, automationTestTag}) {
		t.Fatalf("tag owners = %v", change.TagOwners)
	}
	rules := api.postedSSH(t)
	if len(rules) != 2 || countRules(rules, sshActionCheck, mesh.ServerproPolicyOwner) != 1 {
		t.Fatalf("posted ssh rules = %+v", rules)
	}
	accept := rules[1]
	if accept.Action != sshActionAccept || strings.Join(accept.Src, ",") != mesh.AutomationSourceTag || strings.Join(accept.Dst, ",") != automationTestTag || strings.Join(accept.Users, ",") != automationTestAdmin {
		t.Fatalf("accept rule = %+v", accept)
	}
	owners := api.posted["tagOwners"].(map[string]any)
	if got := owners[mesh.AutomationSourceTag].([]any); len(got) != 1 || got[0] != mesh.ServerproPolicyOwner {
		t.Fatalf("automation tag owners = %#v", owners)
	}
	if strings.Join(api.requests, ",") != "GET /tailnet/-/acl,POST /tailnet/-/acl/validate,POST /tailnet/-/acl" {
		t.Fatalf("requests = %v", api.requests)
	}
}

// sharedAutomationPolicy is a policy where the accept rule already exists
// next to a foreign rule with a field serverpro does not model.
const sharedAutomationPolicy = `{
	"tagOwners": {"tag:serverpro-prod": ["autogroup:admin"], "tag:serverpro-automation": ["autogroup:admin"]},
	"ssh": [
		{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["deploy"]},
		{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["deploy"]},
		{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["deploy"]},
		{"action":"check","src":["group:sre"],"dst":["tag:other"],"users":["ops"],"checkPeriod":"12h"}
	]
}`

// Servers with the same tags and admin user share one accept rule, so a
// server with automation off must not revoke it in the ordinary write; only
// the registry-checked revoke may.
func TestEnsureServerproPolicyKeepsSharedAutomationRuleWhenOff(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, sharedAutomationPolicy)

	change, err := client.EnsureServerproPolicy(context.Background(), []string{automationTestTag}, automationTestAdmin, automationTestRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if change.Changed() {
		t.Fatalf("change = %+v", change)
	}
	if strings.Join(api.requests, ",") != "GET /tailnet/-/acl" {
		t.Fatalf("requests = %v", api.requests)
	}
}

// The revoke must remove every exact copy, or a left-over copy keeps
// unattended access open. Foreign rules, their extra fields and the human
// check rule must survive, because the policy is tailnet-wide.
func TestRemoveAutomationSSHRuleRemovesEveryCopy(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, sharedAutomationPolicy)

	change, err := client.RemoveAutomationSSHRule(context.Background(), []string{automationTestTag}, automationTestAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if !change.AutomationSSHRuleRemoved || change.AutomationSSHRule || change.SSHRule || len(change.TagOwners) != 0 {
		t.Fatalf("change = %+v", change)
	}
	rules := api.postedSSH(t)
	if len(rules) != 2 || countRules(rules, sshActionAccept, mesh.AutomationSourceTag) != 0 || countRules(rules, sshActionCheck, mesh.ServerproPolicyOwner) != 1 || countRules(rules, sshActionCheck, "group:sre") != 1 {
		t.Fatalf("posted ssh rules = %+v", rules)
	}
	if !strings.Contains(api.doc, `"checkPeriod":"12h"`) {
		t.Fatalf("foreign rule field lost: %s", api.doc)
	}
	if strings.Join(api.requests, ",") != "GET /tailnet/-/acl,POST /tailnet/-/acl/validate,POST /tailnet/-/acl" {
		t.Fatalf("requests = %v", api.requests)
	}
}

// A revoke must match the exact rule shape: an accept rule for other tags or
// another user belongs to another server, and with nothing to remove there
// must be no write at all.
func TestRemoveAutomationSSHRuleNoopsWithoutExactRule(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, `{"ssh":[
		{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod","tag:serverpro-web"],"users":["deploy"]},
		{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["ops"]}
	]}`)

	change, err := client.RemoveAutomationSSHRule(context.Background(), []string{automationTestTag}, automationTestAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if change.Changed() || strings.Join(api.requests, ",") != "GET /tailnet/-/acl" {
		t.Fatalf("change=%+v requests=%v", change, api.requests)
	}
}

// Without a policy ETag the write is refused, so a revoke cannot overwrite a
// concurrent edit made in the admin console.
func TestRemoveAutomationSSHRuleRequiresETag(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/acl":
			_, _ = io.WriteString(w, sharedAutomationPolicy)
		case "POST /tailnet/-/acl":
			posts++
		}
	}))
	t.Cleanup(srv.Close)

	_, err := NewWithHTTP("token", "-", srv.URL, srv.Client()).RemoveAutomationSSHRule(context.Background(), []string{automationTestTag}, automationTestAdmin)
	if !errors.Is(err, ErrPolicyETagMissing) || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

// A hand-made duplicate accept rule is collapsed to one while automation is
// on, so state and the live policy describe one rule.
func TestEnsureServerproPolicyDropsDuplicateAutomationRule(t *testing.T) {
	accept := `{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["deploy"]}`
	api, client := newAutomationPolicyAPI(t, `{
		"tagOwners": {"tag:serverpro-prod": ["autogroup:admin"], "tag:serverpro-automation": ["autogroup:admin"]},
		"ssh": [{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["deploy"]}, `+accept+`, `+accept+`]
	}`)

	change, err := client.EnsureServerproPolicy(context.Background(), []string{automationTestTag}, automationTestAdmin, automationTestRoot, mesh.AutomationSourceTag)
	if err != nil {
		t.Fatal(err)
	}
	if change.AutomationSSHRule || !change.AutomationSSHRuleRemoved {
		t.Fatalf("change = %+v", change)
	}
	if rules := api.postedSSH(t); countRules(rules, sshActionAccept, mesh.AutomationSourceTag) != 1 {
		t.Fatalf("posted ssh rules = %+v", rules)
	}
}

// An already-correct policy must cause no write: each POST is a tailnet-wide
// change that other operators see in the audit log.
func TestEnsureServerproPolicyNoopsWhenAutomationRuleExists(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, `{
		"tagOwners": {"tag:serverpro-prod": ["autogroup:admin"], "tag:serverpro-automation": ["autogroup:admin"]},
		"ssh": [
			{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["deploy"]},
			{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["deploy"]}
		]
	}`)

	change, err := client.EnsureServerproPolicy(context.Background(), []string{automationTestTag}, automationTestAdmin, automationTestRoot, mesh.AutomationSourceTag)
	if err != nil {
		t.Fatal(err)
	}
	if change.Changed() {
		t.Fatalf("change = %+v", change)
	}
	if strings.Join(api.requests, ",") != "GET /tailnet/-/acl" {
		t.Fatalf("requests = %v", api.requests)
	}
}

// Only the fixed automation tag may be an accept source; a server tag there
// would let every server in a namespace log in to its peers without check.
// The refusal must come before any API call.
func TestEnsureServerproPolicyRejectsOtherAutomationSource(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, `{"ssh":[]}`)

	_, err := client.EnsureServerproPolicy(context.Background(), []string{automationTestTag}, automationTestAdmin, automationTestRoot, automationTestTag)
	if err == nil || !strings.Contains(err.Error(), "unsupported automation SSH source") || !strings.Contains(err.Error(), mesh.AutomationSourceTag) {
		t.Fatalf("err = %v", err)
	}
	if len(api.requests) != 0 {
		t.Fatalf("requests = %v", api.requests)
	}
}

// Doctor compares rules from the admin console, which stores HuJSON, so the
// SSH read must accept comments and keep every typed rule field.
func TestSSHPolicyReadsHuJSONRules(t *testing.T) {
	_, client := newAutomationPolicyAPI(t, `{
		// admin console comment
		"ssh": [
			{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["deploy"],},
		],
	}`)

	rules, err := client.SSHPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Action != sshActionAccept || rules[0].Src[0] != mesh.AutomationSourceTag || rules[0].Dst[0] != automationTestTag || rules[0].Users[0] != automationTestAdmin {
		t.Fatalf("rules = %+v", rules)
	}
}

// The post-write validation must fail closed when automation is on but the
// accept rule did not land, because unattended runs would then hang on a
// browser login instead of failing with a clear error.
func TestValidateSSHPolicyRequiresAutomationAcceptRule(t *testing.T) {
	check := `{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["deploy"]}`
	accept := `{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["deploy"]}`
	for _, tc := range []struct {
		name    string
		doc     string
		wantErr string
	}{
		{name: "missing accept", doc: `{"ssh":[` + check + `]}`, wantErr: "no accept rule"},
		{name: "accept present", doc: `{"ssh":[` + check + `,` + accept + `]}`},
		{name: "root accept", doc: `{"ssh":[` + check + `,` + accept + `,{"action":"accept","src":["tag:serverpro-automation"],"dst":["tag:serverpro-prod"],"users":["root"]}]}`, wantErr: "root access"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newAutomationPolicyAPI(t, tc.doc)
			err := client.ValidateSSHPolicy(context.Background(), []string{automationTestTag}, automationTestAdmin, automationTestRoot, mesh.AutomationSourceTag)
			if tc.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
