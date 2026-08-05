package lifecycle

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
)

func TestRunReportsPartialComputeCheckpointSaveFailure(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")
	cfg.Cloudflare.AccountID = "acc"
	path := provisionStatePath(t)
	provider := &checkpointSaveFailCompute{path: path}

	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, ComputeAccount: compute.Account{Name: "prod", Provider: "hetzner", Token: "h"}, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: path, Clients: Clients{Compute: provider, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err == nil || !strings.Contains(err.Error(), "server create failed") || !strings.Contains(err.Error(), "partial compute checkpoint failed") {
		t.Fatalf("expected provider and checkpoint errors, got %v", err)
	}
}

type checkpointSaveFailCompute struct {
	recordingCompute
	path string
}

func (f *checkpointSaveFailCompute) Create(_ context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	if err := os.Remove(f.path); err != nil {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	if err := os.Mkdir(f.path, 0o700); err != nil {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	return compute.ServerRecord{Provider: "hetzner", Account: request.Account.Name, Namespace: request.Intent.Namespace, Server: request.Intent.Server, Name: request.Intent.Name, ProviderState: map[string]string{"access_policy_id": "9"}}, compute.Diagnostics{{Status: compute.Fail, Message: "server create failed"}}
}
