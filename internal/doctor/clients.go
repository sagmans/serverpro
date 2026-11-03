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
	WaitDevice(context.Context, mesh.DeviceQuery) (mesh.Device, error)
}

// TailscaleDNSClient introspects tailnet DNS posture. Capability stays
// optional so lighter fakes and non-API clients remain valid TailscaleClients.
type TailscaleDNSClient interface {
	DNSConfig(context.Context) (mesh.DNSConfig, error)
}

// TailnetPacketPolicyClient reads the tailnet policy's acls and grants. It is
// optional like TailscaleDNSClient, and a separate method from the preflight
// Policy read so fakes that audit preflight calls do not see doctor reads.
type TailnetPacketPolicyClient interface {
	PacketPolicy(context.Context) (mesh.PacketPolicy, error)
}

// TailnetSSHPolicyClient reads the tailnet SSH rules. It is optional like the
// packet policy read, and a separate method for the same reason.
type TailnetSSHPolicyClient interface {
	SSHPolicy(context.Context) ([]mesh.SSHRule, error)
}

// TailnetSSHPolicyFixer is the policy write that provision uses. doctor --fix
// calls it, so a repair leaves the same rules as a create.
type TailnetSSHPolicyFixer interface {
	EnsureServerproPolicy(context.Context, []string, string, string, string) (mesh.PolicyChange, error)
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
