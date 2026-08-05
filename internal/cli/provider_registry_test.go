package cli

import (
	"testing"

	"github.com/assagman/serverpro/internal/compute"
)

func TestProviderRegistryIncludesBuiltInProviders(t *testing.T) {
	providers := (&app{}).providerRegistry().List()
	got := make(map[compute.ProviderName]bool, len(providers))
	for _, provider := range providers {
		got[provider.Name()] = true
	}
	for _, want := range []compute.ProviderName{"digitalocean", "hetzner", "vultr"} {
		if !got[want] {
			t.Fatalf("provider %q missing from registry: %+v", want, got)
		}
	}
}
