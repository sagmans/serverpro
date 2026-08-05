package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunRequiresComputeProvider(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api"}, StatePath: provisionStatePath(t), Clients: Clients{Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "compute provider required") {
		t.Fatalf("expected compute provider required error, got %v", err)
	}
}

func TestRunCreatesServerThroughComputeProviderAndWritesGenericState(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	provider := &recordingCompute{}
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, ComputeAccount: compute.Account{Name: "prod", Provider: "hetzner", Token: "h"}, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: provider, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatal(err)
	}
	if provider.request.Intent.Namespace != "prod" || provider.request.Intent.Server != "web" || provider.request.Intent.Name != "prod-web" {
		t.Fatalf("bad intent identity: %+v", provider.request.Intent)
	}
	if provider.request.Intent.Location != "fsn1" || provider.request.Intent.Size != "cx23" || provider.request.Intent.Image != "ubuntu-24.04" {
		t.Fatalf("bad generic catalog values: %+v", provider.request.Intent)
	}
	if provider.request.Intent.Labels["serverpro-namespace"] != "prod" || provider.request.Intent.Labels["serverpro-server"] != "web" {
		t.Fatalf("missing provider ownership labels: %+v", provider.request.Intent.Labels)
	}
	if provider.request.Intent.Labels["project"] != "" {
		t.Fatalf("ambiguous project label leaked into compute intent: %+v", provider.request.Intent.Labels)
	}
	if !strings.Contains(provider.request.BootstrapData, "tskey-auth-created") || !strings.Contains(provider.request.BootstrapData, testAdminPasswordHash) {
		t.Fatalf("bootstrap data missing secure access setup")
	}
	if strings.Contains(provider.request.BootstrapData, "ts-api") || strings.Contains(provider.request.BootstrapData, "cf") {
		t.Fatalf("bootstrap data leaked provider token")
	}
	if provider.request.Account.Name != "prod" || provider.request.Account.Token != "h" {
		t.Fatalf("compute account was not passed to provider: %+v", provider.request.Account)
	}
	if st.Compute.Provider != "hetzner" || st.Compute.Account != "" || st.Compute.ID != "2" || st.Compute.ProviderState["access_policy_id"] != "1" {
		t.Fatalf("generic compute state missing: %+v", st.Compute)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["compute"]; !ok {
		t.Fatalf("state missing compute key: %s", raw)
	}
	if _, ok := decoded["hetzner"]; ok {
		t.Fatalf("state contains provider-specific top-level key: %s", raw)
	}
}

func TestRunCheckpointsProviderStateWhenComputeCreateFails(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	provider := &failingCheckpointCompute{}

	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, ComputeAccount: compute.Account{Name: "prod", Provider: "hetzner", Token: "h"}, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: provider, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "server create failed") {
		t.Fatalf("expected compute create failure, got %v", err)
	}
	st, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Compute.ProviderState["access_policy_id"] != "9" || st.Compute.ID != "" {
		t.Fatalf("partial provider state not checkpointed: %+v", st.Compute)
	}
}

func TestRunCheckpointsProviderStateBeforeComputeCreationContinues(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	path := provisionStatePath(t)
	provider := &checkpointBeforeCompute{statePath: path}

	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, ComputeAccount: compute.Account{Name: "prod", Provider: "hetzner", Token: "h"}, Creds: credentials.Set{Tailscale: "ts-api"}, StatePath: path, Clients: Clients{Compute: provider, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !provider.checkpointObserved {
		t.Fatal("compute creation continued before provider state was durably checkpointed")
	}
}

type checkpointBeforeCompute struct {
	recordingCompute
	statePath          string
	checkpointObserved bool
}

func (f *checkpointBeforeCompute) Create(_ context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	if request.CheckpointProviderState == nil {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider state checkpoint missing"}}
	}
	if err := request.CheckpointProviderState(map[string]string{"access_policy_id": "9"}); err != nil {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	st, err := state.Load(f.statePath)
	if err != nil || st.Compute.ProviderState["access_policy_id"] != "9" {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider state was not durable before compute creation"}}
	}
	f.checkpointObserved = true
	return f.recordingCompute.Create(context.Background(), request)
}

type failingCheckpointCompute struct{ recordingCompute }

func (f *failingCheckpointCompute) Create(_ context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	return compute.ServerRecord{Provider: "hetzner", Account: request.Account.Name, Namespace: request.Intent.Namespace, Server: request.Intent.Server, Name: request.Intent.Name, ProviderState: map[string]string{"access_policy_id": "9"}}, compute.Diagnostics{{Status: compute.Fail, Message: "server create failed"}}
}
