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
		ID: "abc", Name: "demo-dev", PublicIPv4: "203.0.113.20",
		Labels: ownership.ProviderLabels("demo", "dev", nil),
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
	if !strings.Contains(out.String(), `"server": "dev"`) || !strings.Contains(out.String(), `"namespace": "demo"`) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestServerImportAllWritesLocalState(t *testing.T) {
	createTestHome(t)
	provider := listImportProvider{records: []compute.ServerRecord{{
		Provider: "vultr", ID: "abc", Name: "demo-dev", Location: "ewr", Size: "vc2-1c-1gb", Image: "2284",
		PublicIPv4: "203.0.113.20",
		Labels:     ownership.ProviderLabels("demo", "dev", nil),
		ProviderState: map[string]string{
			"firewall_group_id": "fw-1",
		},
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
	st, err := state.Load(config.ServerStatePath("demo", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Compute.ID != "abc" || st.Compute.Provider != "vultr" || st.Compute.ProviderState["firewall_group_id"] != "fw-1" {
		t.Fatalf("state=%+v", st)
	}
}

func TestServerImportWritesFailedRowsBeforeReturningError(t *testing.T) {
	createTestHome(t)
	provider := listImportProvider{records: []compute.ServerRecord{{
		Provider: "vultr", ID: "abc", Name: "demo-dev", Location: "ewr", Size: "vc2-1c-1gb",
		Labels: ownership.ProviderLabels("demo", "dev", nil),
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
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--admin-user", "ops"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "1 server import failed") {
		t.Fatalf("expected failed import exit, got %v", err)
	}
	var results []map[string]any
	if jsonErr := json.Unmarshal(out.Bytes(), &results); jsonErr != nil {
		t.Fatalf("output=%s err=%v", out.String(), jsonErr)
	}
	if len(results) != 1 || results[0]["status"] != "failed" {
		t.Fatalf("results=%+v", results)
	}
}

func TestServerImportRequiresProvider(t *testing.T) {
	createTestHome(t)
	a := &app{stdout: io.Discard, stderr: io.Discard, all: true, yes: true, providers: compute.NewRegistry()}
	if err := a.serverImportCmd().Execute(); err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("err=%v", err)
	}
}
