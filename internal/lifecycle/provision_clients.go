package lifecycle

import (
	"context"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/remote"
)

type TailscalePolicyInspector interface {
	InspectServerproPolicyParts(context.Context, []string, []string, string) (tailscale.ServerproPolicyChange, error)
}

type TailscaleClient interface {
	TailscalePolicyInspector
	TailnetID(context.Context) (string, error)
	CreateAuthKey(context.Context, []string, time.Duration) (tailscale.AuthKey, error)
	DeleteAuthKey(context.Context, string) error
	EnsureServerproPolicy(context.Context, []string, string, string, tailscale.PolicyCheckpoint) (tailscale.ServerproPolicyChange, error)
	ValidateSSHPolicy(context.Context, []string, string, string) error
	MatchingDeviceIDs(context.Context, string, []string) ([]string, error)
	WaitDevice(context.Context, tailscale.DeviceWait) (tailscale.Device, error)
}

type CloudflareClient interface {
	CreateTunnel(context.Context, string) (cloudflare.Tunnel, error)
	TunnelToken(context.Context, string) (string, error)
}

type Clients struct {
	Compute    compute.Provider
	Tailscale  TailscaleClient
	Cloudflare CloudflareClient
	Remote     remote.Runner
}
