package mesh

import (
	"encoding/json"
	"testing"
)

func TestSSHRuleUnmarshalJSONRetainsUnknownFields(t *testing.T) {
	var rule SSHRule
	if err := json.Unmarshal([]byte(`{"action":"accept","src":["group:admins"],"dst":["tag:old"],"users":["root"],"checkPeriod":"always","future":{"enabled":true}}`), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.Action != "accept" || len(rule.Dst) != 1 || rule.Dst[0] != "tag:old" || len(rule.extra) != 2 {
		t.Fatalf("rule=%+v extra=%+v", rule, rule.extra)
	}
	if string(rule.extra["checkPeriod"]) != `"always"` || string(rule.extra["future"]) != `{"enabled":true}` {
		t.Fatalf("unknown fields changed: %+v", rule.extra)
	}
	if err := json.Unmarshal([]byte(`{"action":`), &rule); err == nil {
		t.Fatal("malformed rule should fail")
	}
}

func TestSSHRuleMarshalJSONRestoresUnknownFields(t *testing.T) {
	rule := SSHRule{
		Action: "accept",
		Src:    []string{"group:admins"},
		Dst:    []string{"tag:new"},
		Users:  []string{"root"},
		extra:  map[string]json.RawMessage{"checkPeriod": json.RawMessage(`"always"`)},
	}
	payload, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["dst"]) != `["tag:new"]` || string(fields["checkPeriod"]) != `"always"` {
		t.Fatalf("payload=%s", payload)
	}
}
