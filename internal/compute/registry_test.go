package compute

import (
	"context"
	"testing"
)

type fakeProvider struct {
	name ProviderName
}

func (p fakeProvider) Name() ProviderName { return p.name }
func (p fakeProvider) Capabilities(context.Context) Capabilities {
	return Capabilities{CreateServer: true, Catalog: true}
}
func (p fakeProvider) Doctor(context.Context, Account) Diagnostics {
	return Diagnostics{{Status: Pass, Message: "ok"}}
}
func (p fakeProvider) Catalog(context.Context, CatalogQuery) (Catalog, Diagnostics) {
	return Catalog{Locations: []Location{{Name: "fsn1"}}}, nil
}
func (p fakeProvider) List(context.Context, ListServersQuery) ([]ServerRecord, Diagnostics) {
	return nil, nil
}
func (p fakeProvider) Create(context.Context, CreateServerRequest) (ServerRecord, Diagnostics) {
	return ServerRecord{}, nil
}
func (p fakeProvider) Status(context.Context, ServerRef) (ServerStatus, Diagnostics) {
	return ServerStatus{}, nil
}
func (p fakeProvider) Power(context.Context, PowerRequest) (ServerStatus, Diagnostics) {
	return ServerStatus{}, nil
}
func (p fakeProvider) Delete(context.Context, DeleteServerRequest) Diagnostics { return nil }

func TestRegistryListsAndReturnsProviders(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fakeProvider{name: "hetzner"}); err != nil {
		t.Fatal(err)
	}
	providers := registry.List()
	if len(providers) != 1 || providers[0].Name() != "hetzner" {
		t.Fatalf("providers=%+v", providers)
	}
	provider, ok := registry.Get("hetzner")
	if !ok || provider.Name() != "hetzner" {
		t.Fatalf("provider lookup failed: %+v ok=%t", provider, ok)
	}
}

func TestDiagnosticsErrorUsesGenericMessage(t *testing.T) {
	diagnostics := Diagnostics{{Status: Fail, Message: "credential rejected"}}
	if diagnostics.Passed() || diagnostics.Err() == nil || diagnostics.Err().Error() != "credential rejected" {
		t.Fatalf("bad diagnostics: %+v err=%v", diagnostics, diagnostics.Err())
	}
}
