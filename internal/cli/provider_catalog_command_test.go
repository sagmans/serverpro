package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
)

type cliFakeProvider struct{}

func (cliFakeProvider) Name() compute.ProviderName { return "hetzner" }
func (cliFakeProvider) SupportsImageReference(image string) bool {
	return image == "ubuntu-24.04"
}
func (cliFakeProvider) Capabilities(context.Context) compute.Capabilities {
	return compute.Capabilities{CreateServer: true, DeleteServer: true, PowerServer: true, Catalog: true}
}
func (cliFakeProvider) Doctor(context.Context, compute.Account) compute.Diagnostics {
	return compute.Diagnostics{{Status: compute.Pass, Message: "credential valid"}}
}
func (cliFakeProvider) Catalog(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	return compute.Catalog{
		Locations: []compute.Location{{Name: "fsn1", City: "Falkenstein", Country: "DE"}},
		Sizes:     []compute.Size{{Name: "cpx22", Cores: 2, MemoryGB: 4, DiskGB: 80}},
		Images:    []compute.Image{{Name: "ubuntu-24.04", Architecture: "x86", OSFlavor: "ubuntu", OSVersion: "24.04"}},
	}, nil
}
func (cliFakeProvider) List(context.Context, compute.ListServersQuery) ([]compute.ServerRecord, compute.Diagnostics) {
	return nil, nil
}
func (cliFakeProvider) Create(context.Context, compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	return compute.ServerRecord{}, nil
}
func (cliFakeProvider) Status(context.Context, compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	return compute.ServerStatus{}, nil
}
func (cliFakeProvider) Power(context.Context, compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	return compute.ServerStatus{}, nil
}
func (cliFakeProvider) Delete(context.Context, compute.DeleteServerRequest) compute.Diagnostics {
	return nil
}

func testProviderRegistry(t *testing.T) *compute.Registry {
	t.Helper()
	registry := compute.NewRegistry()
	if err := registry.Register(cliFakeProvider{}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestProviderCommandsUseComputeRegistry(t *testing.T) {
	createTestHome(t)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, providers: testProviderRegistry(t)}
	cmd := a.providerCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var providers []providerRow
	if err := json.Unmarshal(out.Bytes(), &providers); err != nil {
		t.Fatalf("provider list is not JSON:\n%s", out.String())
	}
	if len(providers) != 1 || providers[0].Name != "hetzner" {
		t.Fatalf("provider list missing hetzner: %+v", providers)
	}

	out.Reset()
	cmd = a.providerCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"status", "hetzner"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var status providerStatusRow
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("provider status is not JSON:\n%s", out.String())
	}
	if status.Name != "hetzner" || !status.Capabilities.Catalog || !status.Capabilities.CreateServer {
		t.Fatalf("bad provider status: %+v", status)
	}
}

func TestEphemeralComputeAccountCachesPromptedProviderToken(t *testing.T) {
	createTestHome(t)
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "")
	t.Setenv("SERVER_PROVIDER_TOKEN", "")
	var prompts bytes.Buffer
	a := &app{stdin: strings.NewReader("provider-token\n"), stdout: io.Discard, stderr: &prompts, jsonOut: true}
	provider := cliFakeProvider{}
	first, err := a.ephemeralComputeAccount(provider)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.ephemeralComputeAccount(provider)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token != "provider-token" || second.Token != "provider-token" {
		t.Fatalf("cached provider token not reused: first=%+v second=%+v", first, second)
	}
	if count := strings.Count(prompts.String(), "server provider API token:"); count != 1 {
		t.Fatalf("provider token prompted %d times:\n%s", count, prompts.String())
	}
}

func TestEnsureCredentialsReusesPromptedProviderCatalogToken(t *testing.T) {
	createTestHome(t)
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "")
	t.Setenv("SERVER_PROVIDER_TOKEN", "")
	var prompts bytes.Buffer
	a := &app{provider: "hetzner", stdin: strings.NewReader("provider-token\nts-token\n"), stdout: io.Discard, stderr: &prompts, jsonOut: true}
	if _, err := a.ephemeralComputeAccount(cliFakeProvider{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.ExampleServer("demo", "web")
	creds, saved, err := a.ensureCredentials(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !saved || creds.ServerProvider != "provider-token" || creds.Tailscale != "ts-token" {
		t.Fatalf("cached token not saved with credentials: saved=%t creds=%+v", saved, creds)
	}
	loaded, err := credentials.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ServerProvider != "provider-token" || loaded.Tailscale != "ts-token" {
		t.Fatalf("bad saved credentials: %+v", loaded)
	}
	if count := strings.Count(prompts.String(), "server provider API token:"); count != 1 {
		t.Fatalf("provider token prompted %d times:\n%s", count, prompts.String())
	}
}

func TestCatalogCommandsUseEphemeralProviderToken(t *testing.T) {
	createTestHome(t)
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "secret")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"locations"}, "fsn1"},
		{[]string{"sizes", "--location", "fsn1"}, "cpx22"},
		{[]string{"images", "--location", "fsn1"}, "ubuntu-24.04"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out bytes.Buffer
			a := &app{stdout: &out, stderr: io.Discard, provider: "hetzner", providers: testProviderRegistry(t)}
			cmd := a.catalogCmd()
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), "secret") {
				t.Fatalf("bad catalog output:\n%s", out.String())
			}
		})
	}
}
