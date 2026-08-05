package doctor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

type cloudflareTunnelGetter interface {
	GetTunnel(context.Context, string) (cloudflare.Tunnel, error)
}

func providerInventory(ctx context.Context, cfg config.Config, st state.State, creds credentials.Set, clients Clients, opt Options) []InventoryItem {
	items := make([]InventoryItem, 0, 3)
	items = append(items, computeInventory(ctx, cfg, st, opt.ComputeAccount, clients.Compute)...)
	items = append(items, tailscaleInventory(ctx, cfg, st, creds.Tailscale, clients.Tailscale)...)
	items = append(items, cloudflareInventory(ctx, st, clients.Cloudflare)...)
	return items
}

func computeInventory(ctx context.Context, cfg config.Config, st state.State, account compute.Account, client ComputeClient) []InventoryItem {
	if client == nil || st.Compute.ID == "" {
		return nil
	}
	status, diagnostics := client.Status(ctx, compute.ServerRef{Account: account, Record: computeRecordFromState(st)})
	if !diagnostics.Passed() || !computeLabelsMatchTarget(status.Record.Labels, cfg, st) {
		return nil
	}
	return []InventoryItem{{Scope: "provider", Name: "compute server", Value: computeServerInventory(status)}}
}

func computeServerInventory(status compute.ServerStatus) string {
	record := status.Record
	return fmt.Sprintf("id=%s name=%s power=%s size=%s location=%s ipv4=%s ipv6=%s", valueOrUnknown(record.ID), valueOrUnknown(record.Name), valueOrUnknown(status.Power), valueOrUnknown(record.Size), valueOrUnknown(record.Location), valueOrUnknown(status.PublicIPv4), valueOrUnknown(status.PublicIPv6))
}

func tailscaleInventory(ctx context.Context, cfg config.Config, st state.State, token string, client TailscaleClient) []InventoryItem {
	if token == "" || client == nil {
		return nil
	}
	tsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dev, err := client.WaitDevice(tsCtx, tailscale.DeviceWait{Hostname: tailscaleLookupName(cfg, st), Tags: cfg.Access.Tailscale.Tags, DeviceID: st.Tailscale.NodeID})
	if err != nil {
		return nil
	}
	return []InventoryItem{{Scope: "provider", Name: "tailscale node", Value: fmt.Sprintf("name=%s hostname=%s api_reported_online=%t control_connected=%t addresses=%s tags=%s", valueOrUnknown(dev.Name), valueOrUnknown(dev.Hostname), dev.Online, dev.ConnectedToControl, joinOrUnknown(dev.Addresses), joinOrUnknown(dev.Tags))}}
}

func cloudflareInventory(ctx context.Context, st state.State, client CloudflareClient) []InventoryItem {
	if client == nil || st.Cloudflare.TunnelID == "" {
		return nil
	}
	value := fmt.Sprintf("id=%s name=%s", st.Cloudflare.TunnelID, valueOrUnknown(st.Cloudflare.Name))
	if getter, ok := client.(cloudflareTunnelGetter); ok {
		if tunnel, err := getter.GetTunnel(ctx, st.Cloudflare.TunnelID); err == nil {
			value = fmt.Sprintf("id=%s name=%s status=%s created=%s", tunnel.ID, valueOrUnknown(tunnel.Name), valueOrUnknown(tunnel.Status), valueOrUnknown(tunnel.CreatedAt))
		}
	}
	return []InventoryItem{{Scope: "provider", Name: "cloudflare tunnel", Value: value}}
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func joinOrUnknown(values []string) string {
	if len(values) == 0 {
		return "unknown"
	}
	return strings.Join(values, ",")
}
