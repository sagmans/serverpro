package cli

import (
	"testing"

	"github.com/assagman/serverpro/internal/compute"
)

func TestImageChoicesFromCatalogOnlyOffersBootstrapCompatibleImages(t *testing.T) {
	choices := imageChoicesFromCatalog([]compute.Image{
		{Name: "ubuntu-24.04", OSFlavor: "ubuntu", OSVersion: "24.04"},
		{Name: "ubuntu-22.04", OSFlavor: "ubuntu", OSVersion: "22.04"},
		{Name: "debian-12", OSFlavor: "debian", OSVersion: "12"},
	})
	if len(choices) != 1 || choices[0].Value != "ubuntu-24.04" {
		t.Fatalf("unsupported choices exposed: %+v", choices)
	}
}
