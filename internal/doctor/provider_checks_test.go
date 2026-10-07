package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

type offlineCloudflare struct{}

type timedOutCloudflare struct{}

type deadlineCheckingTailscale struct {
	deadlineSeen bool
}

func (offlineCloudflare) GetTunnel(context.Context, string) (ingress.Tunnel, error) {
	return ingress.Tunnel{Status: "inactive"}, nil
}
func (timedOutCloudflare) GetTunnel(context.Context, string) (ingress.Tunnel, error) {
	return ingress.Tunnel{}, context.DeadlineExceeded
}

func (d *deadlineCheckingTailscale) WaitDevice(ctx context.Context, q mesh.DeviceQuery) (tailscale.Device, error) {
	_, d.deadlineSeen = ctx.Deadline()
	return tailscale.Device{Name: q.Hostname, Online: true, ConnectedToControl: true}, nil
}

func TestProviderInventoryUsesBoundedTailscaleLookup(t *testing.T) {
	cfg := config.Example("prod")
	client := &deadlineCheckingTailscale{}
	items := tailscaleInventory(context.Background(), cfg, state.State{Tailscale: state.TailscaleState{Name: "prod-01"}}, "ts-token-long", client)
	if !client.deadlineSeen {
		t.Fatal("tailscale inventory should set a lookup deadline")
	}
	if len(items) != 1 || !strings.Contains(items[0].Value, "api_reported_online=true") {
		t.Fatalf("missing tailscale inventory: %+v", items)
	}
}

type identityTailscale struct {
	query mesh.DeviceQuery
	err   error
}

func (c *identityTailscale) WaitDevice(_ context.Context, q mesh.DeviceQuery) (mesh.Device, error) {
	c.query = q
	return mesh.Device{}, c.err
}

// Doctor must check the recorded device, and an identity conflict needs its
// own code so automation does not treat it as an ordinary offline node.
func TestTailscaleNodeCheckBindsRecordedDeviceAndCodesIdentityConflicts(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Tailscale: state.TailscaleState{Name: "prod-01.example.ts.net", NodeID: "n-recorded"}}
	client := &identityTailscale{err: mesh.ErrBoundDeviceMissing}
	res := checkTailscaleNode(context.Background(), cfg, st, "ts-token-long", client)
	if client.query.NodeID != "n-recorded" {
		t.Fatalf("query = %+v, want recorded node id", client.query)
	}
	if res.Status != Fail || res.Code != TailscaleDeviceIdentityCode {
		t.Fatalf("result = %+v", res)
	}

	client.err = context.DeadlineExceeded
	if res := checkTailscaleNode(context.Background(), cfg, st, "ts-token-long", client); res.Status != Fail || res.Code != "" {
		t.Fatalf("offline result = %+v, want uncoded failure", res)
	}
}

// With --fix, remote repair pipes the sudo password to the recorded name. An
// identity conflict must stop every remote command before that can happen.
func TestDoctorSendsNothingRemoteWhenDeviceIdentityFails(t *testing.T) {
	cfg := config.Example("prod")
	st := doctorState(cfg, "", "")
	st.Tailscale.NodeID = "n-recorded"
	r := &fakeRemote{}
	report := RunWithOptions(context.Background(), cfg, st, credentials.Set{Tailscale: "ts-token-long"}, Clients{Compute: fakeCompute{}, Tailscale: &identityTailscale{err: mesh.ErrBoundDeviceMissing}, Cloudflare: fakeCloudflare{}, Remote: r, PublicSSHProbe: refusedPublicSSHProbe}, Options{Fix: true, SudoPassword: "sudo-secret"})
	if len(r.commands) != 0 {
		t.Fatalf("remote commands sent despite identity failure: %d", len(r.commands))
	}
	if !hasResult(report, remoteChecksBlockedName, Skip, "no command or credential") {
		t.Fatalf("missing blocked remote result: %+v", report.Results)
	}
}

func TestCloudflareInventoryRequiresClient(t *testing.T) {
	items := cloudflareInventory(context.Background(), state.State{Cloudflare: state.CloudflareState{TunnelID: "tun1", Name: "from-state"}}, nil)
	if len(items) != 0 {
		t.Fatalf("cloudflare provider inventory without client = %+v", items)
	}
}

func TestProviderChecksRejectUnmanagedComputeResources(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Namespace: "prod", Server: cfg.Server, Compute: state.ComputeState{Provider: "hetzner", Account: "prod", ID: "2", Name: "prod-01"}}
	client := fakeCompute{status: compute.ServerStatus{Record: compute.ServerRecord{ID: "2", Name: "prod-01", Labels: map[string]string{"serverpro.namespace": "other"}}}}
	if res := checkComputeServer(context.Background(), cfg, st, compute.Account{Name: "prod", Provider: "hetzner"}, client); res.Status != Fail || res.Name != "compute ownership" {
		t.Fatalf("expected unmanaged server failure, got %+v", res)
	}
}

func TestProviderChecksAcceptManagedAccessPolicy(t *testing.T) {
	st := state.State{Compute: state.ComputeState{ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "fw-1"}}}}
	if res := checkComputeAccessPolicy(st); res.Status != Pass {
		t.Fatalf("expected access policy pass, got %+v", res)
	}
}

func TestCloudflareConnectorWarnsWhenOffline(t *testing.T) {
	res := checkCloudflareConnector(context.Background(), "tun1", offlineCloudflare{})
	if res.Status != Warn || res.Name != "cloudflare connector" {
		t.Fatalf("expected offline connector warning, got %+v", res)
	}
}

func TestCloudflareConnectorWarnsOnTimeout(t *testing.T) {
	res := checkCloudflareConnector(context.Background(), "tun1", timedOutCloudflare{})
	if res.Status != Warn || res.Name != "cloudflare tunnel" || !strings.Contains(res.Evidence, "timed out") {
		t.Fatalf("expected timeout warning, got %+v", res)
	}
}
