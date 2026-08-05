package doctor

import (
	"context"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func Run(ctx context.Context, cfg config.Config, st state.State, creds credentials.Set, clients Clients) Report {
	return RunWithOptions(ctx, cfg, st, creds, clients, Options{})
}

func RunWithOptions(ctx context.Context, cfg config.Config, st state.State, creds credentials.Set, clients Clients, opt Options) Report {
	clients = snapshotProviderClients(clients)
	inventory := providerInventory(ctx, cfg, st, creds, clients, opt)
	inventory = append(inventory, remoteInventory(ctx, clients.Remote, cfg.Admin.Username, st.Tailscale.Name)...)
	rs := []Result{
		localTool("tailscale"),
		localTool("ssh"),
		stateSecretsClean(st, creds),
		checkComputeServer(ctx, cfg, st, opt.ComputeAccount, clients.Compute),
		checkComputeAccessPolicy(st),
	}
	for _, ip := range publicSSHAddresses(ctx, cfg, st, opt.ComputeAccount, clients.Compute) {
		rs = append(rs, publicSSHClosedWithProbe(ctx, ip, clients.PublicSSHProbe))
	}
	rs = append(rs, checkTailscaleNode(ctx, cfg, st, creds.Tailscale, clients.Tailscale))
	rs = append(rs, checkTailscaleDNS(ctx, creds.Tailscale, clients.Tailscale))
	rs = append(rs, remoteChecksWithOptions(ctx, cfg, clients.Remote, st.Tailscale.Name, opt)...)
	rs = annotateTailscaleSSHStatus(rs)
	rs = append(rs, checkCloudflareConnector(ctx, st.Cloudflare.TunnelID, clients.Cloudflare))
	return Report{Inventory: inventory, Results: rs}
}

func publicSSHAddresses(ctx context.Context, cfg config.Config, st state.State, account compute.Account, client ComputeClient) []string {
	ipv4 := st.Compute.PublicIPv4
	ipv6 := st.Compute.PublicIPv6
	if client != nil && st.Compute.ID != "" {
		status, diagnostics := client.Status(ctx, compute.ServerRef{Account: account, Record: computeRecordFromState(st)})
		if diagnostics.Passed() && computeLabelsMatchTarget(status.Record.Labels, cfg, st) {
			if status.PublicIPv4 != "" {
				ipv4 = status.PublicIPv4
			} else if status.Record.PublicIPv4 != "" {
				ipv4 = status.Record.PublicIPv4
			}
			if status.PublicIPv6 != "" {
				ipv6 = status.PublicIPv6
			} else if status.Record.PublicIPv6 != "" {
				ipv6 = status.Record.PublicIPv6
			}
		}
	}
	return uniqueNonEmpty(ipv4, ipv6)
}

func uniqueNonEmpty(values ...string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func annotateTailscaleSSHStatus(results []Result) []Result {
	sshOK := false
	for _, result := range results {
		if result.Scope == "remote" && result.Status == Pass {
			sshOK = true
			break
		}
	}
	if !sshOK {
		return results
	}
	for i := range results {
		if results[i].Scope == "provider" && results[i].Name == "tailscale node" && strings.Contains(results[i].Evidence, "api_reported_online=false") && !strings.Contains(results[i].Evidence, "ssh=ok") {
			results[i].Evidence += " ssh=ok"
		}
	}
	return results
}
