package digitalocean

import (
	"github.com/sagmans/serverpro/internal/cloudinit"
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
		if !size.Available || (location != "" && !stringInSlice(location, size.Regions)) {
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
		if !image.Public || image.Slug == "" || image.Status != "available" || (location != "" && !stringInSlice(location, image.Regions)) {
			continue
		}
		mapped := compute.Image{
			Name:         image.Slug,
			Description:  image.Name,
			Architecture: imageArchitecture(image.Slug),
			OSFlavor:     image.Distribution,
		}
		if cloudinit.SupportsImage(mapped) {
			out.Images = append(out.Images, mapped)
		}
	}
	return out
}

func stringInSlice(needle string, haystack []string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return len(haystack) == 0
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
