package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/hostplatform"
)

type catalogChoiceSet struct {
	locations []choice
	sizes     []choice
	images    []choice
}

func (a *app) liveCatalogChoices(location string) (catalogChoiceSet, bool, error) {
	if a.provider == "" {
		return catalogChoiceSet{}, false, nil
	}
	provider, err := a.resolveProvider(a.provider)
	if err != nil {
		return catalogChoiceSet{}, false, err
	}
	accountRef, err := a.ephemeralComputeAccount(provider)
	if err != nil {
		return catalogChoiceSet{}, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerCatalogTimeout)
	defer cancel()
	catalog, diagnostics := provider.Catalog(ctx, compute.CatalogQuery{Account: accountRef, Location: location})
	if !diagnostics.Passed() {
		return catalogChoiceSet{}, false, diagnostics.Err()
	}
	return catalogChoiceSet{
		locations: locationChoicesFromCatalog(catalog.Locations),
		sizes:     sizeChoicesFromCatalog(catalog.Sizes),
		images:    imageChoicesFromCatalog(catalog.Images),
	}, true, nil
}

func locationChoicesFromCatalog(items []compute.Location) []choice {
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		choices = append(choices, choice{Value: item.Name, Description: compactDescription(item.Description, item.City, item.Country, item.Zone)})
	}
	return choices
}

func sizeChoicesFromCatalog(items []compute.Size) []choice {
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		description := item.Description
		if description == "" {
			description = fmt.Sprintf("%d vCPU · %g GB RAM · %d GB disk · %s", item.Cores, item.MemoryGB, item.DiskGB, item.Architecture)
		}
		choices = append(choices, choice{Value: item.Name, Description: description})
	}
	return choices
}

func imageChoicesFromCatalog(items []compute.Image) []choice {
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		if !isSupportedManagedImage(item) {
			continue
		}
		choices = append(choices, choice{Value: item.Name, Description: compactDescription(item.Description, item.OSFlavor, item.OSVersion, item.Architecture)})
	}
	return choices
}

func validateManagedImageCatalog(catalog compute.Catalog, selected string) error {
	found := false
	for _, image := range catalog.Images {
		if image.Name != selected {
			continue
		}
		found = true
		if isSupportedManagedImage(image) {
			return nil
		}
	}
	if !found {
		return fmt.Errorf("managed image %q is not present in provider catalog", selected)
	}
	return fmt.Errorf("unsupported managed image %q; require %s %s on %s", selected, hostplatform.ManagedHostOS, hostplatform.ManagedHostVersion, strings.Join(hostplatform.ManagedHostArchitectures(), " or "))
}

func isSupportedManagedImage(image compute.Image) bool {
	flavor := strings.ToLower(strings.TrimSpace(image.OSFlavor))
	if flavor != hostplatform.ManagedHostOS {
		return false
	}
	architecture := strings.ToLower(strings.TrimSpace(image.Architecture))
	if !slices.Contains(hostplatform.ManagedHostImageArchitectures(), architecture) {
		return false
	}
	if image.OSVersion != "" {
		return strings.TrimSpace(image.OSVersion) == hostplatform.ManagedHostVersion
	}
	identity := strings.ToLower(image.Name + " " + image.Description)
	slugVersion := strings.ReplaceAll(hostplatform.ManagedHostVersion, ".", "-")
	return strings.Contains(identity, hostplatform.ManagedHostOS+"-"+slugVersion) ||
		strings.Contains(identity, hostplatform.ManagedHostOS+" "+hostplatform.ManagedHostVersion)
}

func compactDescription(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}
