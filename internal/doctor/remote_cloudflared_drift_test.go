package doctor

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/tunnel"
)

// TestRemoteCloudflaredSpecificationsFollowConfigIngress pins the split with
// ingress none: the unmanaged audit keeps its repair, while the ingress
// verdict is read-only and fails only a live or unclassifiable connector.
func TestRemoteCloudflaredSpecificationsFollowConfigIngress(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = configIngressNone
	specifications := remoteCloudflaredSpecifications(cfg)
	if len(specifications) != 2 || !slices.Contains(specifications[0].liveCommands, tunnel.UnmanagedRepairScript()) {
		t.Fatalf("ingress none specifications = %+v, want the unmanaged audit then the ingress check", specifications)
	}
	ingress := specifications[1]
	if len(ingress.liveCommands) != 0 || !slices.Contains(ingress.refreshAfterLive, tunnel.UnmanagedRepairScript()) {
		t.Fatalf("ingress check = %+v, want read-only and re-read after the unmanaged repair", ingress)
	}
	state := tunnel.HostStateCommand()
	for _, tc := range []struct {
		name            string
		call            remoteCall
		want            Status
		wantRemediation string
	}{
		{name: "no connector", call: remoteCall{out: tunnel.HostStateAnswer(tunnel.HostStateAbsent)}, want: Pass},
		{name: "dormant package", call: remoteCall{out: tunnel.HostStateAnswer(tunnel.HostStateDormant)}, want: Pass},
		{name: "live connector", call: remoteCall{out: tunnel.HostStateAnswer(tunnel.HostStateActive)}, want: Fail, wantRemediation: cloudflaredOutsideConfigRemediation},
		{name: "unclassifiable host", call: remoteCall{out: "cloudflared is running outside cloudflared.service", err: errors.New("exit status 1")}, want: Fail, wantRemediation: cloudflaredOutsideConfigRemediation},
		{name: "garbled answer", call: remoteCall{out: "ok"}, want: Fail, wantRemediation: driftProbeFailedRemediation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &scriptedRemote{responses: map[string][]remoteCall{state: {tc.call}}}
			results := ingress.run(context.Background(), runner, "deploy", "prod-01", Options{})
			if len(results) != 1 || results[0].Name != tunnel.IngressCheckName || results[0].Status != tc.want || results[0].Remediation != tc.wantRemediation {
				t.Fatalf("results = %+v, want %s with remediation %q", results, tc.want, tc.wantRemediation)
			}
		})
	}

	cfg.Network.Ingress = "cloudflare-tunnel"
	for _, specification := range remoteCloudflaredSpecifications(cfg) {
		if slices.Contains(specification.readCommands, state) || slices.Contains(specification.readCommands, tunnel.UnmanagedCheckCommand()) {
			t.Fatalf("managed ingress plans an unmanaged read: %+v", specification.readCommands)
		}
	}
}
