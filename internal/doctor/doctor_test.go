package doctor

import (
	"context"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
)

type fakeCompute struct {
	status      compute.ServerStatus
	diagnostics compute.Diagnostics
}

func (f fakeCompute) Status(_ context.Context, ref compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	if f.diagnostics != nil {
		return f.status, f.diagnostics
	}
	status := f.status
	if status.Record.ID == "" {
		status.Record = ref.Record
	}
	if status.Record.ID == "" {
		status.Record.ID = "2"
	}
	if status.Record.Name == "" {
		status.Record.Name = "prod-01"
	}
	if status.Record.Size == "" {
		status.Record.Size = "cpx22"
	}
	if status.Record.Location == "" {
		status.Record.Location = "fsn1"
	}
	if status.Record.PublicIPv4 == "" {
		status.Record.PublicIPv4 = "192.0.2.10"
	}
	if status.Record.PublicIPv6 == "" {
		status.Record.PublicIPv6 = "2001:db8::1"
	}
	if status.Power == "" {
		status.Power = "running"
	}
	if status.PublicIPv4 == "" {
		status.PublicIPv4 = status.Record.PublicIPv4
	}
	if status.PublicIPv6 == "" {
		status.PublicIPv6 = status.Record.PublicIPv6
	}
	return status, compute.Diagnostics{{Status: compute.Pass, Message: "ok"}}
}

type fakeTailscale struct{}

func (fakeTailscale) WaitDevice(context.Context, tailscale.DeviceWait) (tailscale.Device, error) {
	return tailscale.Device{Name: "prod-01.tailnet.ts.net", Hostname: "prod-01", Addresses: []string{"100.64.0.1"}, Tags: []string{"tag:serverpro-server"}, Online: false, ConnectedToControl: true}, nil
}

type fakeCloudflare struct{}

func (fakeCloudflare) ConnectorOnline(context.Context, string) (bool, error) { return true, nil }
func (fakeCloudflare) GetTunnel(context.Context, string) (cloudflare.Tunnel, error) {
	return cloudflare.Tunnel{ID: "tun1", Name: "prod-tunnel", Status: "healthy", CreatedAt: "2026-06-05T00:00:00Z"}, nil
}

type fakeRemote struct {
	commands []string
	errs     map[string]error
}

func (f *fakeRemote) Run(_ context.Context, user, host, script string) (string, error) {
	f.commands = append(f.commands, script)
	if f.errs != nil && f.errs[script] != nil {
		return "", f.errs[script]
	}
	return "ok", nil
}

func hasResult(report Report, name string, status Status, evidenceSub string) bool {
	for _, r := range report.Results {
		if r.Name == name && r.Status == status && strings.Contains(r.Evidence, evidenceSub) {
			return true
		}
	}
	return false
}

func hasInventory(report Report, name string, valueSub string) bool {
	for _, item := range report.Inventory {
		if item.Name == name && strings.Contains(item.Value, valueSub) {
			return true
		}
	}
	return false
}

func hasEvidence(report Report, sub string) bool {
	for _, r := range report.Results {
		if strings.Contains(r.Evidence, sub) {
			return true
		}
	}
	return false
}

func hasCommand(commands []string, sub string) bool {
	for _, c := range commands {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}
