package cli

import "testing"

func TestProviderCatalogTimeoutIsBounded(t *testing.T) {
	if providerCatalogTimeout <= 0 {
		t.Fatal("provider catalog timeout must be positive")
	}
}
