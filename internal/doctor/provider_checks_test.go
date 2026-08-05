package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

type offlineCloudflare struct{}

type timedOutCloudflare struct{}

type deadlineCheckingTailscale struct {
	deadlineSeen bool
}

func (offlineCloudflare) ConnectorOnline(context.Context, string) (bool, error) { return false, nil }
func (timedOutCloudflare) ConnectorOnline(context.Context, string) (bool, error) {
	return false, context.DeadlineExceeded
}

func (d *deadlineCheckingTailscale) WaitDevice(ctx context.Context, request tailscale.DeviceWait) (tailscale.Device, error) {
	_, d.deadlineSeen = ctx.Deadline()
	return tailscale.Device{ID: "device-1", Name: request.Hostname, Online: true, ConnectedToControl: true}, nil
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

func TestCloudflareInventoryRequiresClient(t *testing.T) {
	items := cloudflareInventory(context.Background(), state.State{Cloudflare: state.CloudflareState{TunnelID: "tun1", Name: "from-state"}}, nil)
	if len(items) != 0 {
		t.Fatalf("cloudflare provider inventory without client = %+v", items)
	}
}

func TestProviderChecksRejectUnmanagedComputeResources(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Project: "prod", Server: cfg.Server, Compute: state.ComputeState{Provider: "hetzner", Account: "prod", ID: "2", Name: "prod-01"}}
	client := fakeCompute{status: compute.ServerStatus{Record: compute.ServerRecord{ID: "2", Name: "prod-01", Labels: map[string]string{"serverpro.namespace": "other"}}}}
	if res := checkComputeServer(context.Background(), cfg, st, compute.Account{Name: "prod", Provider: "hetzner"}, client); res.Status != Fail || res.Name != "compute ownership" {
		t.Fatalf("expected unmanaged server failure, got %+v", res)
	}
}

func TestComputeLabelsMatchTargetRejectsMissingLiveLabels(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Project: "prod", Server: cfg.Server}
	if computeLabelsMatchTarget(nil, cfg, st) || computeLabelsMatchTarget(map[string]string{}, cfg, st) {
		t.Fatal("missing live ownership labels were accepted")
	}
}

func TestProviderChecksRejectMissingAccessPolicyForExistingServer(t *testing.T) {
	st := state.State{Compute: state.ComputeState{ID: "server-1"}}
	if res := checkComputeAccessPolicy(st); res.Status != Fail {
		t.Fatalf("expected missing access policy failure, got %+v", res)
	}
}

func TestProviderChecksAcceptProviderSpecificAccessPolicyKeys(t *testing.T) {
	for _, providerState := range []map[string]string{
		{"access_policy_id": "1"},
		{"firewall_id": "fw-1"},
		{"firewall_group_id": "fw-1"},
	} {
		st := state.State{Compute: state.ComputeState{ProviderState: providerState}}
		if res := checkComputeAccessPolicy(st); res.Status != Pass {
			t.Fatalf("expected access policy pass for %+v, got %+v", providerState, res)
		}
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
