package doctor

import (
	"context"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/remote"
)

type ComputeClient interface {
	Status(context.Context, compute.ServerRef) (compute.ServerStatus, compute.Diagnostics)
}

type TailscaleClient interface {
	WaitDevice(context.Context, tailscale.DeviceWait) (tailscale.Device, error)
}

type CloudflareClient interface {
	ConnectorOnline(context.Context, string) (bool, error)
}

type Clients struct {
	Compute    ComputeClient
	Tailscale  TailscaleClient
	Cloudflare CloudflareClient
	Remote     remote.Runner
}
