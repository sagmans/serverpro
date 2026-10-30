package doctor

import (
	"context"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/tunnel"
)

const (
	// configIngressNone is config's value for "no public app ingress".
	configIngressNone = "none"
	// cloudflaredOutsideConfigRemediation applies to every connector result
	// while config ingress is none: doctor --fix repairs only a connector that
	// config owns, so the drift has to be resolved first.
	cloudflaredOutsideConfigRemediation = "config ingress is none: either set network.ingress to cloudflare-tunnel so serverpro owns this connector, or stop and remove it (sudo systemctl disable --now cloudflared; sudo apt-get purge cloudflared) and delete its tunnel in Cloudflare"
)

// remoteCloudflaredSpecifications covers the connector whatever config says.
// With tunnel ingress the managed check keeps its repair and the pin gains
// one. With ingress none the unmanaged check audits and repairs whatever
// connector the host carries, and the ingress check flags a live one, because
// a passing audit only proves the connector is patched, not that it belongs.
func remoteCloudflaredSpecifications(cfg config.Config) []remoteCheckSpecification {
	if cfg.Network.Ingress != configIngressNone {
		pin := remoteFixableSpecification(tunnel.PinCheckName, tunnel.PinCheckCommand(), tunnel.PinRepairScript())
		// The connector repair rewrites the pin too, so after it ran the batch
		// baseline is stale and the pin is read live instead of re-pinned.
		pin.refreshAfterLive = []string{tunnel.UpgradeScript()}
		return []remoteCheckSpecification{remoteCloudflaredSpecification(tunnel.CheckCommand(), tunnel.UpgradeScript()), pin}
	}
	return []remoteCheckSpecification{
		remoteCloudflaredSpecification(tunnel.UnmanagedCheckCommand(), tunnel.UnmanagedRepairScript()),
		remoteCloudflaredIngressSpecification(),
	}
}

// remoteCloudflaredIngressSpecification has no repair: removing a running
// tunnel or adopting it into config is an operator decision. It reads the
// host live after the unmanaged repair, which can change the host state.
func remoteCloudflaredIngressSpecification() remoteCheckSpecification {
	command := tunnel.HostStateCommand()
	return remoteCheckSpecification{
		readCommands:     []string{command},
		refreshAfterLive: []string{tunnel.UnmanagedRepairScript()},
		run: func(ctx context.Context, runner remote.Runner, user, host string, opt Options) []Result {
			if opt.refreshBaseline {
				// The batch baseline predates the unmanaged repair in this run.
				discardBaseline(runner, command)
			}
			out, err := runner.Run(ctx, user, host, command)
			return []Result{cloudflaredIngressResult(out, err)}
		},
	}
}

// cloudflaredIngressResult fails a live connector and a host the shared
// helper cannot classify (for example a connector process outside the
// service), and passes a dormant package: it serves no traffic, and the
// cloudflared check already holds it to the floor and receipt.
func cloudflaredIngressResult(out string, err error) Result {
	if err != nil {
		return fail("remote", tunnel.IngressCheckName, remoteFailureEvidence(out, err), cloudflaredOutsideConfigRemediation)
	}
	state, ok := tunnel.ParseHostState(out)
	switch {
	case !ok:
		return fail("remote", tunnel.IngressCheckName, "unexpected cloudflared host state output: "+trim(out), driftProbeFailedRemediation)
	case state == tunnel.HostStateActive:
		return fail("remote", tunnel.IngressCheckName, "config ingress is none but cloudflared.service is enabled or running", cloudflaredOutsideConfigRemediation)
	case state == tunnel.HostStateDormant:
		return pass("remote", tunnel.IngressCheckName, "cloudflared installed but neither enabled nor running; config ingress none")
	}
	return pass("remote", tunnel.IngressCheckName, "no cloudflared connector; config ingress none")
}
