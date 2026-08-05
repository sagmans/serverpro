package compute

import "testing"

func TestCatalogShapeCarriesProviderDataOnly(t *testing.T) {
	catalog := Catalog{Locations: []Location{{Name: "loc"}}, Sizes: []Size{{Name: "size"}}, Images: []Image{{Name: "image"}}}
	if len(catalog.Locations) != 1 || len(catalog.Sizes) != 1 || len(catalog.Images) != 1 {
		t.Fatalf("bad catalog shape: %+v", catalog)
	}
}
