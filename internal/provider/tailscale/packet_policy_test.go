package tailscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPacketPolicyDecodesHuJSONPolicyFile uses the admin console's HuJSON
// form, because a strict JSON decode would fail on the comments and trailing
// commas real policy files carry and doctor would never see the rules.
func TestPacketPolicyDecodesHuJSONPolicyFile(t *testing.T) {
	var methods []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{
  // default policy kept from tailnet creation
  "acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]},],
  "grants": [{"src": ["group:admins"], "dst": ["tag:server"], "ip": ["22"]},],
}`))
	}))
	defer ts.Close()

	policy, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).PacketPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if policy.ACLs == nil || len(*policy.ACLs) != 1 || policy.Grants == nil || len(*policy.Grants) != 1 {
		t.Fatalf("policy = %+v", policy)
	}
	if rules := policy.AllowAllRules(); len(rules) != 1 {
		t.Fatalf("AllowAllRules() = %q, want the default acl only", rules)
	}
	if len(methods) != 1 || methods[0] != "GET /tailnet/-/acl" {
		t.Fatalf("requests = %v, want one read-only policy GET", methods)
	}
}

func TestPacketPolicyPropagatesAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
	}))
	defer ts.Close()

	if _, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).PacketPolicy(context.Background()); err == nil {
		t.Fatal("forbidden policy read returned no error")
	}
}
