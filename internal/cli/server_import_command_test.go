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
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/state"
)

type listImportProvider struct {
	cliFakeProvider
	records []compute.ServerRecord
}

func (p listImportProvider) Name() compute.ProviderName { return "vultr" }
func (p listImportProvider) List(context.Context, compute.ListServersQuery) ([]compute.ServerRecord, compute.Diagnostics) {
	return p.records, nil
}

func TestServerDiscoverListsManagedCandidates(t *testing.T) {
	createTestHome(t)
	provider := listImportProvider{records: []compute.ServerRecord{{
		ID: "abc", Name: "example-dev", PublicIPv4: "203.0.113.20",
		Labels: ownership.ProviderLabels("example", "dev", nil),
	}}}
	var out bytes.Buffer
	a := &app{
		stdout:    &out,
		stderr:    io.Discard,
		provider:  "vultr",
		providers: providerRegistryForPower(t, provider),
	}
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "secret")
	cmd := a.serverDiscoverCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"server": "dev"`) || !strings.Contains(out.String(), `"namespace": "example"`) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestServerDiscoverFiltersByServerName(t *testing.T) {
	createTestHome(t)
	provider := listImportProvider{records: []compute.ServerRecord{
		{ID: "abc", Name: "example-dev", Labels: ownership.ProviderLabels("example", "dev", nil)},
		{ID: "def", Name: "example-api", Labels: ownership.ProviderLabels("example", "api", nil)},
	}}
	var out bytes.Buffer
	a := &app{
		stdout:    &out,
		stderr:    io.Discard,
		provider:  "vultr",
		providers: providerRegistryForPower(t, provider),
	}
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "secret")
	cmd := a.serverDiscoverCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--server", "dev"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"server": "dev"`) || strings.Contains(out.String(), `"server": "api"`) {
		t.Fatalf("--server filter not applied: %s", out.String())
	}
}

func TestServerImportAllWritesLocalState(t *testing.T) {
	createTestHome(t)
	provider := listImportProvider{records: []compute.ServerRecord{{
		Provider: "vultr", ID: "abc", Name: "example-dev", Location: "ewr", Size: "vc2-1c-1gb", Image: "2284",
		PublicIPv4:       "203.0.113.20",
		Labels:           ownership.ProviderLabels("example", "dev", nil),
		ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "fw-1"}},
	}}}
	var out bytes.Buffer
	a := &app{
		stdout:    &out,
		stderr:    io.Discard,
		provider:  "vultr",
		all:       true,
		yes:       true,
		providers: providerRegistryForPower(t, provider),
	}
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "provider-token")
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "ts-token")
	cmd := a.serverImportCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--admin-user", "ops"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("output=%s err=%v", out.String(), err)
	}
	if len(results) != 1 || results[0]["status"] != "imported" {
		t.Fatalf("results=%+v", results)
	}
	st, err := state.Load(config.ServerStatePath("example", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	policyID, ok := compute.ManagedResourceID(st.Compute.ManagedResources, compute.ManagedResourceAccessPolicy)
	if st.Compute.ID != "abc" || st.Compute.Provider != "vultr" || !ok || policyID != "fw-1" || len(st.Compute.ProviderState) != 0 {
		t.Fatalf("state=%+v", st)
	}
}

func TestServerImportWithoutTailscaleEnrichmentWritesRecoverableArtifacts(t *testing.T) {
	createTestHome(t)
	provider := listImportProvider{records: []compute.ServerRecord{{
		Provider: "vultr", ID: "abc", Name: "example-dev", Location: "ewr", Size: "vc2-1c-1gb", Image: "2284",
		Labels: ownership.ProviderLabels("example", "dev", nil),
	}}}
	var out bytes.Buffer
	a := &app{
		stdout:    &out,
		stderr:    io.Discard,
		provider:  "vultr",
		all:       true,
		yes:       true,
		providers: providerRegistryForPower(t, provider),
	}
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "provider-token")
	cmd := a.serverImportCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--admin-user", "ops"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.ServerConfigPath("example", "dev"))
	if err != nil {
		t.Fatalf("import wrote invalid config: %v", err)
	}
	if !cfg.Access.Tailscale.Enabled || !cfg.Access.Tailscale.SSH {
		t.Fatalf("import disabled mandatory Tailscale access: %+v", cfg.Access.Tailscale)
	}
	creds, err := credentials.LoadPartial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.ServerProvider != "provider-token" || creds.Tailscale != "" {
		t.Fatalf("partial credentials = %+v", creds)
	}
}

func TestServerImportRequiresProvider(t *testing.T) {
	createTestHome(t)
	a := &app{stdout: io.Discard, stderr: io.Discard, all: true, yes: true, providers: compute.NewRegistry()}
	if err := a.serverImportCmd().Execute(); err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildImportOptionsWiresOptionalServiceEnrichers(t *testing.T) {
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "tailscale-token")
	t.Setenv("SERVERPRO_CLOUDFLARE_TOKEN", "cloudflare-token")
	a := &app{nonInteractive: true}

	opts, err := a.buildImportOptions(nil, "provider-token", &importFlags{
		tailscaleTailnet:    "example.ts.net",
		cloudflareAccountID: "account",
		withTailscale:       true,
		withCloudflare:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.EnrichTailscale == nil || opts.EnrichCloudflare == nil {
		t.Fatalf("optional enrichers not wired: %+v", opts)
	}
}
