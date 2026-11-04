package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/testhttp"
	"github.com/tailscale/hujson"
)

// decodeHuJSONBody lets the fake policy APIs read what serverpro posts: the
// body is the operator's HuJSON file, so plain JSON decoding would fail on
// its comments and trailing commas.
func decodeHuJSONBody(r io.Reader, into any) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	std, err := hujson.Standardize(b)
	if err != nil {
		return err
	}
	return json.Unmarshal(std, into)
}

// postedRuleHasField reports whether a posted ssh rule kept a field that
// serverpro does not model.
func postedRuleHasField(posted map[string]any, field, want string) bool {
	rules, _ := posted["ssh"].([]any)
	for _, raw := range rules {
		if rule, ok := raw.(map[string]any); ok && rule[field] == want {
			return true
		}
	}
	return false
}

// commentedPolicy is a policy in admin-console form. Each comment names the
// entry it belongs to, so a test can tell which comments must survive.
const commentedPolicy = `// header comment
{
	// tag owners comment
	"tagOwners": {
		"tag:custom":          ["autogroup:admin"], // custom owner comment
		"tag:serverpro-stale": ["autogroup:admin"],
	},
	// acls comment
	"acls": [{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"]}],
	"ssh": [
		// human rule comment
		{
			"action": "check",
			"src":    ["autogroup:admin"],
			"dst":    ["tag:serverpro-prod", "tag:serverpro-stale"],
			"users":  ["deploy"],
		},
	],
	// trailing comment
}
`

var commentedPolicyKeptComments = []string{"// header comment", "// tag owners comment", "// custom owner comment", "// acls comment", "// human rule comment", "// trailing comment"}

func requireComments(t *testing.T, doc string, comments []string) {
	t.Helper()
	for _, comment := range comments {
		if !strings.Contains(doc, comment) {
			t.Fatalf("posted policy lost %q:\n%s", comment, doc)
		}
	}
}

// The ordinary write adds entries; every operator comment must stay, and the
// validated bytes must be the posted bytes.
func TestEnsureServerproPolicyKeepsHuJSONComments(t *testing.T) {
	var validated, posted string
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			handlerErr.Record(w, "read body: %v", err)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /tailnet/-/acl":
			w.Header().Set("ETag", automationTestETag)
			_, _ = io.WriteString(w, commentedPolicy)
		case "POST /tailnet/-/acl/validate":
			validated = string(body)
			_, _ = io.WriteString(w, "{}")
		case "POST /tailnet/-/acl":
			posted = string(body)
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	_, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).EnsureServerproPolicy(context.Background(), []string{"tag:serverpro-new"}, []string{"tag:serverpro-new"}, automationTestAdmin, automationTestRoot, mesh.AutomationSourceTag, sshActionCheck)
	if err != nil {
		t.Fatal(err)
	}
	requireComments(t, posted, commentedPolicyKeptComments)
	if validated != posted {
		t.Fatalf("validated body differs from posted body:\n%s\n---\n%s", validated, posted)
	}
	var doc map[string]any
	if err := decodeHuJSONBody(strings.NewReader(posted), &doc); err != nil {
		t.Fatal(err)
	}
	owners := doc["tagOwners"].(map[string]any)
	if owners["tag:serverpro-new"] == nil || owners[mesh.AutomationSourceTag] == nil || owners["tag:serverpro-stale"] == nil {
		t.Fatalf("tagOwners = %#v", owners)
	}
	if rules := doc["ssh"].([]any); len(rules) != 3 {
		t.Fatalf("ssh rules = %#v", rules)
	}
}

// Reconcile trims a rule in place: the rule keeps its comment, and only the
// stale tag owner and its comment-free line go.
func TestApplyServerproPolicyReconcileKeepsHuJSONComments(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, commentedPolicy)
	plan, err := client.PlanServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-prod"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyServerproPolicyReconcile(context.Background(), []string{"tag:serverpro-prod"}, plan); err != nil {
		t.Fatal(err)
	}
	requireComments(t, api.doc, commentedPolicyKeptComments)
	if strings.Contains(api.doc, "tag:serverpro-stale") {
		t.Fatalf("stale tag survived:\n%s", api.doc)
	}
}

func TestPolicyPatchEscapesPointerKeysAndKeepsLiterals(t *testing.T) {
	patch, err := policyPatch([]byte(`{"a/b":1,"c~d":{"n":1.50}}`), []byte(`{"a/b":2,"c~d":{"n":1.50,"s":"<&>"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"op":"replace","path":"/a~1b","value":2},{"op":"add","path":"/c~0d/s","value":"<&>"}]`
	if string(patch) != want {
		t.Fatalf("patch = %s, want %s", patch, want)
	}
}

func TestPolicyPatchEmptyWhenUnchanged(t *testing.T) {
	patch, err := policyPatch([]byte(`{"ssh":[1,2]}`), []byte(`{"ssh":[1,2]}`))
	if err != nil || string(patch) != "[]" {
		t.Fatalf("patch=%s err=%v", patch, err)
	}
}

// Array runs: a removal, a same-shape change diffed in place, an addition.
func TestPolicyPatchArrayOperations(t *testing.T) {
	before := []byte(`{"a":["x","y",{"k":1},"z"]}`)
	after := []byte(`{"a":["x",{"k":2},"z","w"]}`)
	patch, err := policyPatch(before, after)
	if err != nil {
		t.Fatal(err)
	}
	v, err := hujson.Parse(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Patch(patch); err != nil {
		t.Fatalf("patch %s: %v", patch, err)
	}
	if err := requireSameJSON(v, after); err != nil {
		t.Fatalf("patch %s: %v", patch, err)
	}
}

// A huge array is replaced whole instead of building a huge LCS table.
func TestPolicyPatchReplacesHugeArray(t *testing.T) {
	n := 1025
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}
	before, _ := json.Marshal(map[string]any{"a": items})
	items[0] = -1
	after, _ := json.Marshal(map[string]any{"a": items})
	patch, err := policyPatch(before, after)
	if err != nil {
		t.Fatal(err)
	}
	var ops []patchOperation
	if err := json.Unmarshal(patch, &ops); err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Op != patchOpReplace || ops[0].Path != "/a" {
		t.Fatalf("ops = %+v", ops)
	}
}

func TestPolicyPatchRejectsNonObjectRootAndBadJSON(t *testing.T) {
	if _, err := policyPatch([]byte(`{}`), []byte(`[]`)); err == nil {
		t.Fatal("root type change must fail")
	}
	if _, err := policyPatch([]byte(`{`), []byte(`{}`)); err == nil {
		t.Fatal("bad current policy must fail")
	}
	if _, err := policyPatch([]byte(`{}`), []byte(`{`)); err == nil {
		t.Fatal("bad computed policy must fail")
	}
}

func TestRequireSameJSONFailsClosedOnMismatch(t *testing.T) {
	v, err := hujson.Parse([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireSameJSON(v, []byte(`{"a":2}`)); !errors.Is(err, ErrPolicyRenderMismatch) {
		t.Fatalf("err = %v", err)
	}
	if err := requireSameJSON(v, []byte(`{`)); err == nil || errors.Is(err, ErrPolicyRenderMismatch) {
		t.Fatalf("bad computed JSON err = %v", err)
	}
}

func TestReadPolicyFileRejectsInvalidHuJSON(t *testing.T) {
	for name, body := range map[string]string{"syntax": `{"ssh": [`, "array": `[]`, "null": `null`} {
		t.Run(name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("ETag", automationTestETag)
				_, _ = io.WriteString(w, body)
			}))
			defer ts.Close()
			if _, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).readPolicyFile(context.Background()); err == nil {
				t.Fatal("invalid policy must fail")
			}
		})
	}
}
