package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/mesh"
)

// With ssh_tags, doctor counts only the rules for the own tag as this
// server's, and reports an automation rule that reaches it through the
// shared namespace tag: that is another server's access, not this one's.
func TestCheckTailnetSSHPolicyWithSSHTags(t *testing.T) {
	cfg := sshPolicyConfig(true)
	nsTag, ownTag := cfg.Access.Tailscale.Tags[0], "tag:serverpro-prod-webapp"
	cfg.Access.Tailscale.Tags = []string{nsTag, ownTag}
	cfg.Access.Tailscale.SSHTags = []string{ownTag}
	own := []mesh.SSHRule{
		{Action: sshActionCheck, Src: []string{mesh.ServerproPolicyOwner}, Dst: []string{ownTag}, Users: []string{sshPolicyTestAdmin}},
		{Action: sshActionAccept, Src: []string{mesh.AutomationSourceTag}, Dst: []string{ownTag}, Users: []string{sshPolicyTestAdmin}},
	}
	foreign := mesh.SSHRule{Action: sshActionAccept, Src: []string{mesh.AutomationSourceTag}, Dst: []string{nsTag}, Users: []string{"buzz"}}
	for _, tc := range []struct {
		name         string
		rules        []mesh.SSHRule
		want         Status
		wantEvidence string
	}{
		{name: "own rules only", rules: own, want: Pass, wantEvidence: "accept from " + mesh.AutomationSourceTag},
		{name: "other server reaches through namespace tag", rules: append(own[:2:2], foreign), want: Warn, wantEvidence: "other accept rule from " + mesh.AutomationSourceTag + " for [buzz] to [" + nsTag + "]"},
		{name: "old rules on all tags", rules: []mesh.SSHRule{{Action: sshActionCheck, Src: []string{mesh.ServerproPolicyOwner}, Dst: []string{nsTag, ownTag}, Users: []string{sshPolicyTestAdmin}}}, want: Fail, wantEvidence: "human check rule for deploy missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: tc.rules}}
			result := checkTailnetSSHPolicy(context.Background(), cfg, stateWithSource(mesh.AutomationSourceTag), sshPolicyTestToken, client, false)
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
		})
	}
}
