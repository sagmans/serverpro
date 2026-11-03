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
	// policy* keep one tailnet policy read per doctor run, like the DNS read.
	policyOnce sync.Once
	policy     mesh.PacketPolicy
	policyErr  error
}

func (c *snapshotTailscaleClient) WaitDevice(ctx context.Context, q mesh.DeviceQuery) (mesh.Device, error) {
	c.once.Do(func() { c.device, c.err = c.source.WaitDevice(ctx, q) })
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

func (c *snapshotTailscaleClient) PacketPolicy(ctx context.Context) (mesh.PacketPolicy, error) {
	source, ok := c.source.(TailnetPacketPolicyClient)
	if !ok {
		return mesh.PacketPolicy{}, errTailnetPolicyUnsupported
	}
	c.policyOnce.Do(func() { c.policy, c.policyErr = source.PacketPolicy(ctx) })
	return c.policy, c.policyErr
}

// SSHPolicy is read live, not cached: the check reads it once per run, and a
// cached copy would hide a write that another command made since.
func (c *snapshotTailscaleClient) SSHPolicy(ctx context.Context) ([]mesh.SSHRule, error) {
	source, ok := c.source.(TailnetSSHPolicyClient)
	if !ok {
		return nil, errTailnetSSHPolicyUnsupported
	}
	return source.SSHPolicy(ctx)
}

// EnsureServerproPolicy passes the repair through to the live client.
func (c *snapshotTailscaleClient) EnsureServerproPolicy(ctx context.Context, tags, sshTags []string, adminUser, rootPolicy, automationSource string) (mesh.PolicyChange, error) {
	source, ok := c.source.(TailnetSSHPolicyFixer)
	if !ok {
		return mesh.PolicyChange{}, errTailnetSSHPolicyUnsupported
	}
	return source.EnsureServerproPolicy(ctx, tags, sshTags, adminUser, rootPolicy, automationSource)
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
