package state

import "testing"

func TestCloudflareStateOwnsOnlyCreatedTunnel(t *testing.T) {
	tests := []struct {
		name       string
		cloudflare CloudflareState
		want       bool
	}{
		{name: "created", cloudflare: CloudflareState{TunnelID: "tun-1", Provenance: CloudflareTunnelCreated}, want: true},
		{name: "created without identity", cloudflare: CloudflareState{Provenance: CloudflareTunnelCreated}},
		{name: "adopted", cloudflare: CloudflareState{TunnelID: "tun-1", Provenance: CloudflareTunnelAdopted}},
		{name: "imported", cloudflare: CloudflareState{TunnelID: "tun-1", Provenance: CloudflareTunnelImported}},
		{name: "legacy unknown", cloudflare: CloudflareState{TunnelID: "tun-1"}},
		{name: "unknown", cloudflare: CloudflareState{TunnelID: "tun-1", Provenance: "unknown"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.cloudflare.OwnsTunnel(); got != test.want {
				t.Fatalf("OwnsTunnel() = %t, want %t", got, test.want)
			}
		})
	}
}
