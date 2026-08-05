package doctor

import (
	"context"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/remote"
)

type ComputeClient interface {
	Status(context.Context, compute.ServerRef) (compute.ServerStatus, compute.Diagnostics)
}

type TailscaleClient interface {
	WaitDevice(context.Context, string, []string) (mesh.Device, error)
}

// TailscaleDNSClient introspects tailnet DNS posture. Capability stays
// optional so lighter fakes and non-API clients remain valid TailscaleClients.
type TailscaleDNSClient interface {
	DNSConfig(context.Context) (mesh.DNSConfig, error)
}

// PublicSSHProbe separates orchestration tests from real public-network access.
type PublicSSHProbe func(context.Context, string) error

type CloudflareClient interface {
	GetTunnel(context.Context, string) (ingress.Tunnel, error)
}

type Clients struct {
	Compute        ComputeClient
	Tailscale      TailscaleClient
	Cloudflare     CloudflareClient
	Remote         remote.Runner
	PublicSSHProbe PublicSSHProbe
}
