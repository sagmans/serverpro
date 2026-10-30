package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/mesh"
)

type fakeTailnetPolicy struct {
	fakeTailscale
	policy mesh.PacketPolicy
	err    error
	reads  *int
}

func (f fakeTailnetPolicy) PacketPolicy(context.Context) (mesh.PacketPolicy, error) {
	if f.reads != nil {
		*f.reads++
	}
	return f.policy, f.err
}

func TestCheckTailnetPacketPolicy(t *testing.T) {
	scoped := []mesh.Grant{{Src: []string{"group:admins"}, Dst: []string{"tag:server"}, IP: []string{"22"}}}
	allowAll := []mesh.ACLRule{{Action: "accept", Src: []string{"*"}, Dst: []string{"*:*"}}}
	for _, tc := range []struct {
		name         string
		token        string
		client       TailscaleClient
		want         Status
		wantEvidence string
	}{
		{name: "no token", client: fakeTailnetPolicy{}, want: Skip, wantEvidence: "no API token"},
		{name: "client without policy", token: "tok", client: fakeTailscale{}, want: Skip, wantEvidence: "not supported"},
		{name: "snapshot of client without policy", token: "tok", client: snapshotProviderClients(Clients{Tailscale: fakeTailscale{}}).Tailscale, want: Skip, wantEvidence: "not supported"},
		{name: "fetch error", token: "tok", client: fakeTailnetPolicy{err: errors.New("403 forbidden")}, want: Warn, wantEvidence: "403 forbidden"},
		{name: "allow-all", token: "tok", client: fakeTailnetPolicy{policy: mesh.PacketPolicy{ACLs: &allowAll}}, want: Warn, wantEvidence: "acls[0] accepts"},
		{name: "default policy", token: "tok", client: fakeTailnetPolicy{}, want: Warn, wantEvidence: "default allow-all"},
		{name: "scoped grants", token: "tok", client: fakeTailnetPolicy{policy: mesh.PacketPolicy{Grants: &scoped}}, want: Pass, wantEvidence: "no allow-all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := checkTailnetPacketPolicy(context.Background(), tc.token, tc.client)
			if result.Name != tailnetPolicyCheckName || result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
		})
	}
}

func TestSnapshotTailscaleClientReadsPolicyOnce(t *testing.T) {
	reads := 0
	client := snapshotProviderClients(Clients{Tailscale: fakeTailnetPolicy{reads: &reads}}).Tailscale
	for range 2 {
		if result := checkTailnetPacketPolicy(context.Background(), "tok", client); result.Status != Warn {
			t.Fatalf("result = %+v", result)
		}
	}
	if reads != 1 {
		t.Fatalf("policy reads = %d, want 1 per doctor run", reads)
	}
}
