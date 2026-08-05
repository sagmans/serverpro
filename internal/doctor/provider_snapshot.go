package doctor

import (
	"context"
	"errors"
	"sync"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/mesh"
)

func snapshotProviderClients(clients Clients) Clients {
	if clients.Compute != nil {
		clients.Compute = &snapshotComputeClient{source: clients.Compute}
	}
	if clients.Tailscale != nil {
		clients.Tailscale = &snapshotTailscaleClient{source: clients.Tailscale}
	}
	if clients.Cloudflare != nil {
		clients.Cloudflare = &snapshotCloudflareClient{source: clients.Cloudflare}
	}
	return clients
}

type snapshotComputeClient struct {
	source      ComputeClient
	once        sync.Once
	status      compute.ServerStatus
	diagnostics compute.Diagnostics
}

func (c *snapshotComputeClient) Status(ctx context.Context, ref compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	c.once.Do(func() { c.status, c.diagnostics = c.source.Status(ctx, ref) })
	return c.status, c.diagnostics
}

type snapshotTailscaleClient struct {
	source    TailscaleClient
	once      sync.Once
	device    mesh.Device
	err       error
	dnsOnce   sync.Once
	dnsConfig mesh.DNSConfig
	dnsErr    error
}

func (c *snapshotTailscaleClient) WaitDevice(ctx context.Context, name string, tags []string) (mesh.Device, error) {
	c.once.Do(func() { c.device, c.err = c.source.WaitDevice(ctx, name, tags) })
	return c.device, c.err
}

func (c *snapshotTailscaleClient) DNSConfig(ctx context.Context) (mesh.DNSConfig, error) {
	source, ok := c.source.(TailscaleDNSClient)
	if !ok {
		return mesh.DNSConfig{}, errors.New("tailnet DNS introspection not supported")
	}
	c.dnsOnce.Do(func() { c.dnsConfig, c.dnsErr = source.DNSConfig(ctx) })
	return c.dnsConfig, c.dnsErr
}

type snapshotCloudflareClient struct {
	source CloudflareClient
	once   sync.Once
	tunnel ingress.Tunnel
	err    error
}

func (c *snapshotCloudflareClient) GetTunnel(ctx context.Context, id string) (ingress.Tunnel, error) {
	c.once.Do(func() { c.tunnel, c.err = c.source.GetTunnel(ctx, id) })
	return c.tunnel, c.err
}
