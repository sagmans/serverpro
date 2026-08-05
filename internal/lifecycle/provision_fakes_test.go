package lifecycle

import (
	"context"
	"time"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/provider/cloudflare"
	"github.com/assagman/serverpro/internal/provider/tailscale"
)

const testAdminPasswordHash = "$6$rounds=100000$abcdefghijklmnop$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type fakeComputeProvider struct{}

func (fakeComputeProvider) Name() compute.ProviderName { return "hetzner" }
func (fakeComputeProvider) Capabilities(context.Context) compute.Capabilities {
	return compute.Capabilities{CreateServer: true}
}
func (fakeComputeProvider) Doctor(context.Context, compute.Account) compute.Diagnostics { return nil }
func (fakeComputeProvider) Catalog(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	return compute.Catalog{}, nil
}
func (fakeComputeProvider) List(context.Context, compute.ListServersQuery) ([]compute.ServerRecord, compute.Diagnostics) {
	return nil, nil
}
func (fakeComputeProvider) Status(context.Context, compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	return compute.ServerStatus{}, nil
}
func (fakeComputeProvider) Power(context.Context, compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	return compute.ServerStatus{}, nil
}
func (fakeComputeProvider) Delete(context.Context, compute.DeleteServerRequest) compute.Diagnostics {
	return nil
}

type recordingCompute struct {
	fakeComputeProvider
	request     compute.CreateServerRequest
	diagnostics compute.Diagnostics
}

func (f *recordingCompute) Create(_ context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	f.request = request
	return compute.ServerRecord{Provider: "hetzner", Account: request.Account.Name, Namespace: request.Intent.Namespace, Server: request.Intent.Server, ID: "2", Name: request.Intent.Name, Location: request.Intent.Location, Size: request.Intent.Size, Image: request.Intent.Image, PublicIPv4: "203.0.113.10", Labels: request.Intent.Labels, ProviderState: map[string]string{"access_policy_id": "1"}}, f.diagnostics
}

type fakeHetzner struct {
	fakeComputeProvider
	userData        string
	firewallCreated bool
	firewallErr     error
	serverErr       error
	waitErr         error
}

func (f *fakeHetzner) Create(_ context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	f.firewallCreated = true
	if f.firewallErr != nil {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: f.firewallErr.Error()}}
	}
	record := compute.ServerRecord{Provider: "hetzner", Account: request.Account.Name, Namespace: request.Intent.Namespace, Server: request.Intent.Server, Name: request.Intent.Name, Location: request.Intent.Location, Size: request.Intent.Size, Image: request.Intent.Image, Labels: request.Intent.Labels, ProviderState: map[string]string{"access_policy_id": "1"}}
	f.userData = request.BootstrapData
	if f.serverErr != nil {
		return record, compute.Diagnostics{{Status: compute.Fail, Message: f.serverErr.Error()}}
	}
	record.ID = "2"
	record.PublicIPv4 = "203.0.113.10"
	if f.waitErr != nil {
		return record, compute.Diagnostics{{Status: compute.Fail, Message: f.waitErr.Error()}}
	}
	return record, nil
}

type fakeTailscale struct {
	calls         []string
	created       bool
	keyErr        error
	deleteErr     error
	deletedKeyIDs []string
	policyErr     error
	waitErr       error
	baselineIDs   []string
	waitDevice    tailscale.Device
	waitRequests  []tailscale.DeviceWait
	policyChange  tailscale.ServerproPolicyChange
}

func (f *fakeTailscale) CreateAuthKey(context.Context, []string, time.Duration) (tailscale.AuthKey, error) {
	f.calls = append(f.calls, "create-key")
	f.created = true
	if f.keyErr != nil {
		return tailscale.AuthKey{}, f.keyErr
	}
	return tailscale.AuthKey{ID: "k1", Key: "tskey-auth-created"}, nil
}

func (f *fakeTailscale) DeleteAuthKey(_ context.Context, id string) error {
	f.calls = append(f.calls, "delete-key")
	f.deletedKeyIDs = append(f.deletedKeyIDs, id)
	return f.deleteErr
}

func (f *fakeTailscale) EnsureServerproPolicy(_ context.Context, tags []string, _ string, _ string) (tailscale.ServerproPolicyChange, error) {
	f.calls = append(f.calls, "ensure-policy")
	if f.policyErr != nil {
		return tailscale.ServerproPolicyChange{}, f.policyErr
	}
	if len(f.policyChange.TagOwners) > 0 || f.policyChange.SSHRule {
		return f.policyChange, nil
	}
	return tailscale.ServerproPolicyChange{TagOwners: tags, SSHRule: true}, nil
}

func (f *fakeTailscale) ValidateSSHPolicy(context.Context, []string, string, string) error {
	f.calls = append(f.calls, "validate-policy")
	return nil
}

func (f *fakeTailscale) MatchingDeviceIDs(context.Context, string, []string) ([]string, error) {
	f.calls = append(f.calls, "snapshot-devices")
	return append([]string(nil), f.baselineIDs...), nil
}

func (f *fakeTailscale) WaitDevice(_ context.Context, request tailscale.DeviceWait) (tailscale.Device, error) {
	f.calls = append(f.calls, "wait-device")
	f.waitRequests = append(f.waitRequests, request)
	if f.waitErr != nil {
		return tailscale.Device{}, f.waitErr
	}
	if f.waitDevice.ID != "" || f.waitDevice.NodeID != "" {
		return f.waitDevice, nil
	}
	return tailscale.Device{ID: "device-d1", NodeID: "d1", Name: "prod-01", Tags: []string{"tag:serverpro-server"}, Online: true}, nil
}

type fakeCloudflare struct {
	createErr     error
	tokenRequests int
}

func (f fakeCloudflare) CreateTunnel(context.Context, string) (cloudflare.Tunnel, error) {
	if f.createErr != nil {
		return cloudflare.Tunnel{}, f.createErr
	}
	return cloudflare.Tunnel{ID: "tun1", Name: "prod-01"}, nil
}

func (f *fakeCloudflare) TunnelToken(context.Context, string) (string, error) {
	f.tokenRequests++
	return "cf-token", nil
}

type fakeRemote struct {
	scripts []string
	err     error
}

func (f *fakeRemote) Run(_ context.Context, user, host, script string) (string, error) {
	f.scripts = append(f.scripts, script)
	return "ok", f.err
}
