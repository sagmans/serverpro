package digitalocean

import (
	"slices"

	"github.com/sagmans/serverpro/internal/compute"
)

func mapCatalog(catalog Catalog, location string) compute.Catalog {
	out := compute.Catalog{
		Locations: make([]compute.Location, 0, len(catalog.Regions)),
		Sizes:     make([]compute.Size, 0, len(catalog.Sizes)),
		Images:    make([]compute.Image, 0, len(catalog.Images)),
	}
	for _, region := range catalog.Regions {
		if !region.Available {
			continue
		}
		out.Locations = append(out.Locations, compute.Location{Name: region.Slug, Description: region.Name})
	}
	for _, size := range catalog.Sizes {
		// Sizes require explicit region membership: DigitalOcean lists sizes with
		// no regions (for example GPU plans) that a droplet create rejects.
		if !size.Available || (location != "" && !slices.Contains(size.Regions, location)) {
			continue
		}
		out.Sizes = append(out.Sizes, compute.Size{
			Name:         size.Slug,
			Description:  size.Description,
			Cores:        size.VCPUs,
			MemoryGB:     float64(size.Memory) / 1024,
			DiskGB:       size.Disk,
			Architecture: sizeArchitecture(size.Slug),
			Locations:    size.Regions,
		})
	}
	for _, image := range catalog.Images {
		if !image.Public || image.Slug == "" || image.Status != "available" || (location != "" && !offeredIn(image.Regions, location)) {
			continue
		}
		out.Images = append(out.Images, compute.Image{
			Name:         image.Slug,
			Description:  image.Name,
			Architecture: imageArchitecture(image.Slug),
			OSFlavor:     image.Distribution,
		})
	}
	return out
}

// offeredIn reports whether an image is offered in location. DigitalOcean
// leaves regions empty on images available everywhere, so an empty list means
// offered; sizes use slices.Contains instead because a size without regions
// (GPU plans) cannot be ordered in any standard location.
func offeredIn(regions []string, location string) bool {
	return len(regions) == 0 || slices.Contains(regions, location)
}

func sizeArchitecture(slug string) string {
	if hasSlugSuffix(slug, "amd") {
		return "amd"
	}
	if hasSlugSuffix(slug, "intel") {
		return "intel"
	}
	return "shared"
}

func imageArchitecture(slug string) string {
	if hasSlugSuffix(slug, "x64") {
		return "x64"
	}
	if hasSlugSuffix(slug, "arm64") {
		return "arm64"
	}
	return ""
}

func hasSlugSuffix(slug, suffix string) bool {
	return len(slug) > len(suffix) && slug[len(slug)-len(suffix):] == suffix
}
