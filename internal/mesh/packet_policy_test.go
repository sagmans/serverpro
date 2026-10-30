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
		{name: "cidr and member spellings", policy: `{"grants":[{"src":["autogroup:member"],"dst":["0.0.0.0/0"],"ip":["1-65535"]},{"src":["::/0"],"dst":["::/0"],"ip":["0-65535"]}]}`, want: []string{"grants[0] grants autogroup:member to 0.0.0.0/0 on ip 1-65535", "grants[1] grants ::/0 to ::/0 on ip 0-65535"}},
		{name: "acl ipv6 host and port list", policy: `{"acls":[{"action":"accept","src":["0.0.0.0/0"],"dst":["[::/0]:22,1-65535"]}]}`, want: []string{"acls[0] accepts 0.0.0.0/0 to [::/0]:22,1-65535"}},
		{name: "every port of one protocol", policy: `{"grants":[{"src":["*"],"dst":["*"],"ip":["tcp:*"]},{"src":["*"],"dst":["*"],"ip":["udp:*"]},{"src":["*"],"dst":["*"],"ip":["6:1-65535"]},{"src":["*"],"dst":["*"],"ip":["TCP:0-65535"]}]}`, want: []string{"on ip tcp:*", "on ip udp:*", "on ip 6:1-65535", "on ip TCP:0-65535"}},
		{name: "protocol without listener ports or one port", policy: `{"grants":[{"src":["*"],"dst":["*"],"ip":["icmp:*"]},{"src":["*"],"dst":["*"],"ip":["tcp:22"]},{"src":["*"],"dst":["*"],"ip":["*:443"]}]}`},
		{name: "tailnet address ranges", policy: `{"grants":[{"src":["100.64.0.0/10"],"dst":["*"],"ip":["*"]},{"src":["fd7a:115c:a1e0::/48"],"dst":["*"],"ip":["*"]},{"src":["64.0.0.0/2"],"dst":["fd7a:115c:a1e0::/40"],"ip":["*"]}]}`, want: []string{"grants[0] grants 100.64.0.0/10", "grants[1] grants fd7a:115c:a1e0::/48", "grants[2] grants 64.0.0.0/2 to fd7a:115c:a1e0::/40"}},
		{name: "part of a tailnet range or one address", policy: `{"grants":[{"src":["100.64.0.0/11"],"dst":["*"],"ip":["*"]},{"src":["fd7a:115c:a1e0:ab12::/64"],"dst":["*"],"ip":["*"]},{"src":["100.101.102.103"],"dst":["*"],"ip":["*"]},{"src":["*"],"dst":["100.64.0.0/12"],"ip":["*"]}]}`},
		{name: "every tagged device", policy: `{"grants":[{"src":["*"],"dst":["autogroup:tagged"],"ip":["*"]}],"acls":[{"action":"accept","src":["*"],"dst":["autogroup:tagged:*"]}]}`, want: []string{"acls[0] accepts * to autogroup:tagged:*", "grants[0] grants * to autogroup:tagged on ip *"}},
		{name: "tagged devices as source", policy: `{"grants":[{"src":["autogroup:tagged"],"dst":["*"],"ip":["*"]}]}`},
		{name: "hosts aliases", policy: `{"hosts":{"tailnet":"100.64.0.0/10","everything":"::/0","laptop":"100.101.102.103"},"grants":[{"src":["tailnet"],"dst":["*"],"ip":["*"]},{"src":["laptop"],"dst":["*"],"ip":["*"]}],"acls":[{"action":"accept","src":["*"],"dst":["everything:*"]}]}`, want: []string{"acls[0] accepts * to everything (::/0):*", "grants[0] grants tailnet (100.64.0.0/10) to * on ip *"}},
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
