package vultr

import (
	"context"
	"net/http"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
)

func TestComputeProviderCatalogReturnsOnlyProviderSizes(t *testing.T) {
	srv := fakeCatalogServer(t, http.StatusOK)
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	catalog, diagnostics := provider.Catalog(context.Background(), compute.CatalogQuery{Account: compute.Account{Token: "token"}, Location: "ewr"})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if len(catalog.Sizes) == 0 {
		t.Fatal("catalog sizes missing")
	}
}
