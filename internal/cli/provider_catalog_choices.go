package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
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
		choices = append(choices, choice{Value: item.Name, Description: compactDescription(item.Description, item.OSFlavor, item.OSVersion, item.Architecture)})
	}
	return choices
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
