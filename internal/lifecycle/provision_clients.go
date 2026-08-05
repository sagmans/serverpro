package lifecycle

import (
	"context"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/remote"
)

// ComputeCreator isolates provisioning from unrelated provider operations.
type ComputeCreator interface {
	Name() compute.ProviderName
	Create(context.Context, compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics)
}

type TailscaleClient interface {
	CreateAuthKey(context.Context, []string, time.Duration) (mesh.AuthKey, error)
	DeleteAuthKey(context.Context, string) error
	EnsureServerproPolicy(context.Context, []string, string, string) (mesh.PolicyChange, error)
	ValidateSSHPolicy(context.Context, []string, string, string) error
	WaitDevice(context.Context, string, []string) (mesh.Device, error)
}

type CloudflareClient interface {
	ListTunnels(context.Context) ([]ingress.Tunnel, error)
	CreateTunnel(context.Context, string) (ingress.Tunnel, error)
	DeleteTunnel(context.Context, string) error
	TunnelToken(context.Context, string) (string, error)
}

type Clients struct {
	Compute    ComputeCreator
	Tailscale  TailscaleClient
	Cloudflare CloudflareClient
	Remote     remote.Runner
}
