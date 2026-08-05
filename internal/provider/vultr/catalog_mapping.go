package vultr

import (
	"strconv"

	"github.com/assagman/serverpro/internal/cloudinit"
	"github.com/assagman/serverpro/internal/compute"
)

func mapCatalog(catalog Catalog, location string) compute.Catalog {
	out := compute.Catalog{
		Locations: make([]compute.Location, 0, len(catalog.Regions)),
		Sizes:     make([]compute.Size, 0, len(catalog.Plans)),
		Images:    make([]compute.Image, 0, len(catalog.OS)),
	}
	for _, region := range catalog.Regions {
		out.Locations = append(out.Locations, compute.Location{Name: region.ID, City: region.City, Country: region.Country})
	}
	for _, plan := range catalog.Plans {
		if location != "" && !planSupportsLocation(plan, location) {
			continue
		}
		out.Sizes = append(out.Sizes, compute.Size{
			Name:         plan.ID,
			Cores:        plan.VCPUCount,
			MemoryGB:     float64(plan.RAM) / 1024,
			DiskGB:       plan.Disk,
			Architecture: plan.Type,
			Locations:    plan.Locations,
		})
	}
	for _, os := range catalog.OS {
		mapped := compute.Image{
			Name:         strconv.FormatInt(os.ID, 10),
			Description:  os.Name,
			Architecture: os.Arch,
			OSFlavor:     os.Family,
		}
		if cloudinit.SupportsImage(mapped) {
			out.Images = append(out.Images, mapped)
		}
	}
	return out
}

func planSupportsLocation(plan Plan, location string) bool {
	for _, loc := range plan.Locations {
		if loc == location {
			return true
		}
	}
	return len(plan.Locations) == 0
}
