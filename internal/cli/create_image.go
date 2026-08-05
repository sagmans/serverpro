package cli

import (
	"fmt"
	"strings"

	"github.com/assagman/serverpro/internal/cloudinit"
	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
)

func validateCreateImageReference(provider compute.Provider, image string) error {
	policy, ok := provider.(compute.ImageReferencePolicy)
	if !ok || !policy.SupportsImageReference(image) {
		return fmt.Errorf("compute image %q is incompatible with serverpro bootstrap; select provider Ubuntu 24.04 LTS image", image)
	}
	return nil
}

func validateCreateCatalogSelection(catalog compute.Catalog, cfg config.Config) error {
	if !catalogContainsLocation(catalog.Locations, cfg.Compute.Location) {
		return fmt.Errorf("compute location %q is unavailable", cfg.Compute.Location)
	}
	size, ok := catalogSize(catalog.Sizes, cfg.Compute.Size)
	if !ok {
		return fmt.Errorf("compute size %q is unavailable in location %q", cfg.Compute.Size, cfg.Compute.Location)
	}
	foundSupportedImage := false
	for _, image := range catalog.Images {
		if image.Name != cfg.Compute.Image || !cloudinit.SupportsImage(image) {
			continue
		}
		foundSupportedImage = true
		if architecturesCompatible(size.Architecture, image.Architecture) {
			return nil
		}
	}
	if foundSupportedImage {
		return fmt.Errorf("compute image %q architecture is incompatible with size %q architecture", cfg.Compute.Image, cfg.Compute.Size)
	}
	return fmt.Errorf("compute image %q is unavailable or incompatible; serverpro bootstrap requires Ubuntu 24.04 LTS", cfg.Compute.Image)
}

func catalogContainsLocation(items []compute.Location, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func catalogSize(items []compute.Size, name string) (compute.Size, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return compute.Size{}, false
}

func architecturesCompatible(size, image string) bool {
	sizeFamily := architectureFamily(size)
	imageFamily := architectureFamily(image)
	return sizeFamily == "" || imageFamily == "" || sizeFamily == imageFamily
}

func architectureFamily(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "arm", "arm64", "aarch64":
		return "arm"
	case "x86", "x64", "x86_64", "amd64", "amd", "intel":
		return "x86"
	default:
		return ""
	}
}
