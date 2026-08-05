package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/state"
)

func checkComputeServer(ctx context.Context, cfg config.Config, st state.State, account compute.Account, client ComputeClient) Result {
	if st.Compute.ID == "" {
		return fail("state", "compute server", "missing provider server id", "run provision")
	}
	if client == nil {
		return fail("provider", "compute server", "no compute provider", "configure provider")
	}
	status, diagnostics := client.Status(ctx, compute.ServerRef{Account: account, Record: computeRecordFromState(st)})
	if !diagnostics.Passed() {
		return fail("provider", "compute server", diagnostics.Err().Error(), "check account/server id")
	}
	if !computeLabelsMatchTarget(status.Record.Labels, cfg, st) {
		return fail("provider", "compute ownership", "missing expected ownership labels", "do not mutate unmanaged server")
	}
	return pass("provider", "compute server", fmt.Sprintf("id=%s ip=%s", status.Record.ID, status.PublicIPv4))
}

func checkComputeAccessPolicy(st state.State) Result {
	if _, ok := compute.ManagedResourceID(st.Compute.ManagedResources, compute.ManagedResourceAccessPolicy); ok {
		return pass("state", "access policy", "tracked")
	}
	return skip("state", "access policy", "not tracked for this server")
}

func computeRecordFromState(st state.State) compute.ServerRecord {
	return compute.ServerRecord{
		Provider:         compute.ProviderName(st.Compute.Provider),
		Namespace:        st.Compute.Namespace,
		Server:           st.Compute.Server,
		ID:               st.Compute.ID,
		Name:             st.Compute.Name,
		Location:         st.Compute.Location,
		Size:             st.Compute.Size,
		Image:            st.Compute.Image,
		PublicIPv4:       st.Compute.PublicIPv4,
		PublicIPv6:       st.Compute.PublicIPv6,
		Labels:           st.Labels,
		ManagedResources: append([]compute.ManagedResourceRef(nil), st.Compute.ManagedResources...),
		ProviderState:    st.Compute.ProviderState,
	}
}

func computeLabelsMatchTarget(labels map[string]string, cfg config.Config, st state.State) bool {
	if labels == nil {
		return true
	}
	namespace := st.Namespace
	if namespace == "" {
		namespace = cfg.Namespace
	}
	server := st.Server
	if server == "" {
		server = cfg.Server
	}
	return ownership.LiveLabelsMatch(labels, namespace, server)
}

func checkTailscaleNode(ctx context.Context, cfg config.Config, st state.State, token string, client TailscaleClient) Result {
	if token == "" {
		return skip("provider", "tailscale API", "no API token; auth-key-only mode")
	}
	if client == nil {
		return fail("provider", "tailscale node", "no tailscale client", "configure provider")
	}
	tsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	dev, err := client.WaitDevice(tsCtx, tailscaleLookupName(cfg, st), cfg.Access.Tailscale.Tags)
	cancel()
	if err != nil {
		return fail("provider", "tailscale node", err.Error(), "check auth key, tags, ACL/device approval")
	}
	return pass("provider", "tailscale node", fmt.Sprintf("%s api_reported_online=%t control_connected=%t", dev.Name, dev.Online, dev.ConnectedToControl))
}

func tailscaleLookupName(cfg config.Config, st state.State) string {
	if st.Tailscale.Name != "" {
		return st.Tailscale.Name
	}
	if st.Compute.Name != "" {
		return st.Compute.Name
	}
	return cfg.Compute.Name
}

func checkCloudflareConnector(ctx context.Context, tunnelID string, client CloudflareClient) Result {
	if tunnelID == "" {
		return skip("provider", "ingress", "no public ingress configured")
	}
	if client == nil {
		return fail("provider", "cloudflare tunnel", "no cloudflare client", "configure provider")
	}
	cfCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	tunnel, err := client.GetTunnel(cfCtx, tunnelID)
	cancel()
	if err != nil {
		if providerProbeTimedOut(err) {
			return warn("provider", "cloudflare tunnel", "probe timed out: "+trim(err.Error()))
		}
		return fail("provider", "cloudflare tunnel", err.Error(), "check token/account/tunnel")
	}
	if tunnel.Status != "healthy" {
		return warn("provider", "cloudflare connector", "not healthy yet")
	}
	return pass("provider", "cloudflare connector", "healthy")
}

const tailnetDNSCheckName = "tailnet dns"

// checkTailscaleDNS catches the 2026-07 incident posture: MagicDNS enabled with
// zero global nameservers leaves quad100 without an upstream whenever the host
// system-DNS fallback read fails, killing all public resolution silently.
func checkTailscaleDNS(ctx context.Context, token string, client TailscaleClient) Result {
	if token == "" {
		return skip("provider", tailnetDNSCheckName, "no API token; auth-key-only mode")
	}
	if client == nil {
		return skip("provider", tailnetDNSCheckName, "no tailscale client")
	}
	dnsClient, ok := client.(TailscaleDNSClient)
	if !ok {
		return skip("provider", tailnetDNSCheckName, "tailnet DNS introspection not supported by client")
	}
	tsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	cfg, err := dnsClient.DNSConfig(tsCtx)
	cancel()
	if err != nil {
		return warn("provider", tailnetDNSCheckName, "dns config fetch failed: "+err.Error())
	}
	if cfg.MagicDNS && len(cfg.GlobalNameservers) == 0 {
		result := warn("provider", tailnetDNSCheckName, "MagicDNS enabled but no global nameservers configured; quad100 has no upstream")
		result.Remediation = "add tailnet global nameservers in admin console → DNS and enable Override DNS servers"
		return result
	}
	return pass("provider", tailnetDNSCheckName, fmt.Sprintf("magicDNS=%t resolvers=%d", cfg.MagicDNS, len(cfg.GlobalNameservers)))
}

func providerProbeTimedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
