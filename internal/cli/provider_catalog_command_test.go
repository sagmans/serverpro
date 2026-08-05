package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
)

type cliFakeProvider struct {
	doctor func(context.Context, compute.Account) compute.Diagnostics
}

func (cliFakeProvider) Name() compute.ProviderName { return "hetzner" }
func (cliFakeProvider) Capabilities(context.Context) compute.Capabilities {
	return compute.Capabilities{CreateServer: true, DeleteServer: true, PowerServer: true, Catalog: true}
}
func (p cliFakeProvider) Doctor(ctx context.Context, account compute.Account) compute.Diagnostics {
	if p.doctor != nil {
		return p.doctor(ctx, account)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "credential valid"}}
}
func (cliFakeProvider) Catalog(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	return compute.Catalog{
		Locations: []compute.Location{{Name: "fsn1", City: "Falkenstein", Country: "DE"}},
		Sizes:     []compute.Size{{Name: "cpx22", Cores: 2, MemoryGB: 4, DiskGB: 80}},
		Images:    []compute.Image{{Name: "ubuntu-24.04", Architecture: "x86", OSFlavor: "ubuntu"}},
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
	return testRegistryWithProvider(t, cliFakeProvider{})
}

func testRegistryWithProvider(t *testing.T, provider compute.Provider) *compute.Registry {
	t.Helper()
	registry := compute.NewRegistry()
	if err := registry.Register(provider); err != nil {
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

func TestProviderDoctorUsesEphemeralTokenAndWritesDiagnostics(t *testing.T) {
	// WHY: provider doctor is the read-only live API gate. Keep unit coverage on
	// token plumbing and JSON/error behavior while real API reachability remains
	// owned by dogfood.
	createTestHome(t)
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "secret-token")
	called := false
	provider := cliFakeProvider{doctor: func(_ context.Context, account compute.Account) compute.Diagnostics {
		called = true
		if account.Token != "secret-token" || account.Provider != "hetzner" || account.Scope != "ephemeral" {
			t.Fatalf("bad ephemeral account: %+v", account)
		}
		return compute.Diagnostics{{Status: compute.Pass, Message: "credential valid"}}
	}}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, providers: testRegistryWithProvider(t, provider)}
	cmd := a.providerCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"doctor", "hetzner"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("provider Doctor was not called")
	}
	var diagnostics compute.Diagnostics
	if err := json.Unmarshal(out.Bytes(), &diagnostics); err != nil {
		t.Fatalf("provider doctor output is not JSON:\n%s", out.String())
	}
	if len(diagnostics) != 1 || diagnostics[0].Status != compute.Pass || strings.Contains(out.String(), "secret-token") {
		t.Fatalf("bad diagnostics output: %+v\n%s", diagnostics, out.String())
	}
}

func TestProviderDoctorReturnsDiagnosticFailureAfterWritingJSON(t *testing.T) {
	createTestHome(t)
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "secret-token")
	provider := cliFakeProvider{doctor: func(context.Context, compute.Account) compute.Diagnostics {
		return compute.Diagnostics{{Status: compute.Fail, Message: "credential invalid"}}
	}}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, providers: testRegistryWithProvider(t, provider)}
	cmd := a.providerCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"doctor", "hetzner"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "credential invalid") {
		t.Fatalf("expected diagnostic failure, got %v", err)
	}
	if !strings.Contains(out.String(), "credential invalid") || !strings.Contains(out.String(), string(compute.Fail)) {
		t.Fatalf("failure diagnostics not written before returning error:\n%s", out.String())
	}
}

func TestEphemeralComputeAccountCachesPromptedProviderToken(t *testing.T) {
	createTestHome(t)
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "")
	t.Setenv("SERVER_PROVIDER_TOKEN", "")
	var prompts bytes.Buffer
	a := &app{stdin: strings.NewReader("provider-token\n"), stdout: io.Discard, stderr: &prompts}
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
	a := &app{provider: "hetzner", stdin: strings.NewReader("provider-token\nts-token\n"), stdout: io.Discard, stderr: &prompts}
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
