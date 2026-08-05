package importsync

import (
	"context"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/ownership"
	"github.com/assagman/serverpro/internal/state"
)

type listFakeProvider struct {
	records []compute.ServerRecord
	err     string
}

func (listFakeProvider) Name() compute.ProviderName { return "hetzner" }
func (listFakeProvider) Capabilities(context.Context) compute.Capabilities {
	return compute.Capabilities{ListServers: true}
}
func (listFakeProvider) Doctor(context.Context, compute.Account) compute.Diagnostics { return nil }
func (listFakeProvider) Catalog(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	return compute.Catalog{}, nil
}
func (p listFakeProvider) List(context.Context, compute.ListServersQuery) ([]compute.ServerRecord, compute.Diagnostics) {
	if p.err != "" {
		return nil, compute.Diagnostics{{Status: compute.Fail, Message: p.err}}
	}
	return p.records, nil
}
func (listFakeProvider) Create(context.Context, compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	return compute.ServerRecord{}, nil
}
func (listFakeProvider) Status(context.Context, compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	return compute.ServerStatus{}, nil
}
func (listFakeProvider) Power(context.Context, compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	return compute.ServerStatus{}, nil
}
func (listFakeProvider) Delete(context.Context, compute.DeleteServerRequest) compute.Diagnostics {
	return nil
}

func TestDiscoverFiltersManagedAndReportsLocalState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demo", Server: "web", StatePath: "state.json"})
	if err := state.SaveRegistry(defaultRegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
	provider := listFakeProvider{records: []compute.ServerRecord{
		{
			ID: "1", Name: "demo-web",
			Labels: ownership.ProviderLabels("demo", "web", nil),
		},
		{
			ID: "2", Name: "other",
			Labels: map[string]string{"managed-by": "someone-else"},
		},
		{
			ID: "3", Name: "demo-api",
			Labels: ownership.ProviderLabels("demo", "api", nil),
		},
	}}
	candidates, err := Discover(context.Background(), provider, compute.Account{Token: "t"}, DiscoverFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates=%+v", candidates)
	}
	if candidates[0].Server != "api" || candidates[0].LocalState != "missing" {
		t.Fatalf("first=%+v", candidates[0])
	}
	if candidates[1].Server != "web" || candidates[1].LocalState != "present" || !candidates[1].LabelsOK {
		t.Fatalf("second=%+v", candidates[1])
	}
}

func TestDiscoverNamespaceFilter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	provider := listFakeProvider{records: []compute.ServerRecord{
		{ID: "1", Name: "a", Labels: ownership.ProviderLabels("demo", "web", nil)},
		{ID: "2", Name: "b", Labels: ownership.ProviderLabels("other", "web", nil)},
	}}
	candidates, err := Discover(context.Background(), provider, compute.Account{Token: "t"}, DiscoverFilter{Namespace: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Namespace != "demo" {
		t.Fatalf("candidates=%+v", candidates)
	}
}
