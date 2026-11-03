package mesh

import "testing"

// Changed gates the tailnet-wide policy POST. A removal alone must count as a
// change, or turning automation off would skip the write and leave the accept
// rule live.
func TestPolicyChangeChanged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change PolicyChange
		want   bool
	}{
		{name: "nothing", change: PolicyChange{}},
		{name: "tag owner", change: PolicyChange{TagOwners: []string{"tag:serverpro-prod"}}, want: true},
		{name: "check rule", change: PolicyChange{SSHRule: true}, want: true},
		{name: "automation rule added", change: PolicyChange{AutomationSSHRule: true}, want: true},
		{name: "automation rule removed", change: PolicyChange{AutomationSSHRuleRemoved: true}, want: true},
		{name: "empty tag owner list", change: PolicyChange{TagOwners: []string{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.change.Changed(); got != tc.want {
				t.Fatalf("Changed() = %v, want %v", got, tc.want)
			}
		})
	}
}
