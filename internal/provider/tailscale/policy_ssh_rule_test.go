package tailscale

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRemoveSSHRuleRejectsTrackedIdentityDrift(t *testing.T) {
	tags := []string{"tag:serverpro-prod"}
	canonical := serverproSSHRule(tags, "deploy")
	tests := map[string]SSHRule{
		"action":              {Action: "accept", Src: canonical.Src, Dst: canonical.Dst, Users: canonical.Users},
		"added source":        {Action: canonical.Action, Src: []string{serverproPolicyOwner, "group:ops"}, Dst: canonical.Dst, Users: canonical.Users},
		"changed user":        {Action: canonical.Action, Src: canonical.Src, Dst: canonical.Dst, Users: []string{"root"}},
		"changed destination": {Action: canonical.Action, Src: canonical.Src, Dst: []string{"tag:other"}, Users: canonical.Users},
		"added destination":   {Action: canonical.Action, Src: canonical.Src, Dst: []string{"tag:serverpro-prod", "tag:other"}, Users: canonical.Users},
	}
	for name, rule := range tests {
		t.Run(name, func(t *testing.T) {
			doc := policyDocumentWithSSHRules(t, []SSHRule{rule})
			changed, err := doc.removeSSHRule(tags, "deploy")
			if err == nil || !strings.Contains(err.Error(), "ownership drift") {
				t.Fatalf("expected ownership drift, got changed=%t err=%v", changed, err)
			}
		})
	}
}

func TestRemoveSSHRuleDistinguishesExactRuleFromAbsence(t *testing.T) {
	tags := []string{"tag:serverpro-prod"}
	tests := []struct {
		name        string
		rules       []SSHRule
		wantChanged bool
	}{
		{name: "exact", rules: []SSHRule{serverproSSHRule(tags, "deploy")}, wantChanged: true},
		{name: "absent", rules: []SSHRule{{Action: "check", Src: []string{"group:ops"}, Dst: []string{"tag:other"}, Users: []string{"root"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := policyDocumentWithSSHRules(t, test.rules)
			changed, err := doc.removeSSHRule(tags, "deploy")
			if err != nil || changed != test.wantChanged {
				t.Fatalf("changed=%t err=%v", changed, err)
			}
		})
	}
}

func policyDocumentWithSSHRules(t *testing.T, rules []SSHRule) policyDocument {
	t.Helper()
	raw, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	return policyDocument{"ssh": raw}
}
