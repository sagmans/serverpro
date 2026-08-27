package cli

import (
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
)

func TestImageChoicesFromCatalogOnlyOfferSupportedManagedHosts(t *testing.T) {
	choices := imageChoicesFromCatalog([]compute.Image{
		{Name: "debian-12", Architecture: "x86", OSFlavor: "debian", OSVersion: "12"},
		{Name: "ubuntu-22.04", Architecture: "x86", OSFlavor: "ubuntu", OSVersion: "22.04"},
		{Name: "ubuntu-24.04", Architecture: "x86", OSFlavor: "ubuntu", OSVersion: "24.04"},
		{Name: "ubuntu-24-04-arm64", Architecture: "arm64", OSFlavor: "Ubuntu"},
		{Name: "ubuntu-24.04-sparc", Architecture: "sparc", OSFlavor: "ubuntu", OSVersion: "24.04"},
	})
	if len(choices) != 2 || choices[0].Value != "ubuntu-24.04" || choices[1].Value != "ubuntu-24-04-arm64" {
		t.Fatalf("supported catalog image choices = %+v", choices)
	}
}

func TestComputeImageChoicesOnlyOfferSupportedManagedHost(t *testing.T) {
	choices := computeImageChoices()
	if len(choices) != 1 {
		t.Fatalf("compute image choices = %+v, want one supported image", choices)
	}
	if choices[0].Value != "ubuntu-24.04" {
		t.Fatalf("compute image = %q, want Ubuntu 24.04", choices[0].Value)
	}
}
