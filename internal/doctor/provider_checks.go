package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/ownership"
	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/state"
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
	for _, key := range []string{"access_policy_id", "firewall_id", "firewall_group_id"} {
		if st.Compute.ProviderState[key] != "" {
			return pass("state", "access policy", "tracked")
		}
	}
	if st.Compute.ID != "" {
		return fail("state", "access policy", "missing tracked provider access policy", "recover or recreate managed server state")
	}
	return skip("state", "access policy", "compute server not created")
}

func computeRecordFromState(st state.State) compute.ServerRecord {
	return compute.ServerRecord{
		Provider:      compute.ProviderName(st.Compute.Provider),
		Namespace:     st.Compute.Namespace,
		Server:        st.Compute.Server,
		ID:            st.Compute.ID,
		Name:          st.Compute.Name,
		Location:      st.Compute.Location,
		Size:          st.Compute.Size,
		Image:         st.Compute.Image,
		PublicIPv4:    st.Compute.PublicIPv4,
		PublicIPv6:    st.Compute.PublicIPv6,
		Labels:        st.Labels,
		ProviderState: st.Compute.ProviderState,
	}
}

func computeLabelsMatchTarget(labels map[string]string, cfg config.Config, st state.State) bool {
	namespace := st.Namespace
	if namespace == "" {
		namespace = cfg.Project
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
	dev, err := client.WaitDevice(tsCtx, tailscale.DeviceWait{Hostname: tailscaleLookupName(cfg, st), Tags: cfg.Access.Tailscale.Tags, DeviceID: st.Tailscale.NodeID})
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
	ok, err := client.ConnectorOnline(cfCtx, tunnelID)
	cancel()
	if err != nil {
		if providerProbeTimedOut(err) {
			return warn("provider", "cloudflare tunnel", "probe timed out: "+trim(err.Error()))
		}
		return fail("provider", "cloudflare tunnel", err.Error(), "check token/account/tunnel")
	}
	if !ok {
		return warn("provider", "cloudflare connector", "not healthy yet")
	}
	return pass("provider", "cloudflare connector", "healthy")
}

func providerProbeTimedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
