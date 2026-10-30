package mesh

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPacketPolicyAllowAllRules pins the cases doctor must tell apart: an
// absent section is Tailscale's implicit allow-all, an empty one denies, and
// only a rule that opens every source to every port counts as allow-all.
func TestPacketPolicyAllowAllRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy string
		want   []string
	}{
		{name: "no sections", policy: `{"ssh":[]}`, want: []string{"default allow-all"}},
		{name: "empty acls deny", policy: `{"acls":[]}`},
		{name: "default acl", policy: `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`, want: []string{"acls[0] accepts * to *:*"}},
		{name: "legacy users and ports", policy: `{"acls":[{"Action":"accept","Users":["*"],"Ports":["*:*"]}]}`, want: []string{"acls[0]"}},
		{name: "scoped acl", policy: `{"acls":[{"action":"accept","src":["group:admins"],"dst":["tag:server:22"]}]}`},
		{name: "wildcard source to one port", policy: `{"acls":[{"action":"accept","src":["*"],"dst":["*:443"]}]}`},
		{name: "non-accept action", policy: `{"acls":[{"action":"check","src":["*"],"dst":["*:*"]}]}`},
		{name: "allow-all grant", policy: `{"grants":[{"src":["group:admins"],"dst":["tag:server"],"ip":["22"]},{"src":["*"],"dst":["*"],"ip":["*"]}]}`, want: []string{"grants[1] grants * to * on ip *"}},
		{name: "grant limited to ports", policy: `{"grants":[{"src":["*"],"dst":["*"],"ip":["tcp:443"]}]}`},
		{name: "both sections flagged", policy: `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}],"grants":[{"src":["*"],"dst":["*"],"ip":["*"]}]}`, want: []string{"acls[0]", "grants[0]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var policy PacketPolicy
			if err := json.Unmarshal([]byte(tc.policy), &policy); err != nil {
				t.Fatal(err)
			}
			got := policy.AllowAllRules()
			if len(got) != len(tc.want) {
				t.Fatalf("AllowAllRules() = %q, want %d entries like %q", got, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i], want) {
					t.Fatalf("AllowAllRules()[%d] = %q, want it to contain %q", i, got[i], want)
				}
			}
		})
	}
}
