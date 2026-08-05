package hetzner

import "context"

func (c Client) Catalog(ctx context.Context) (Catalog, error) {
	locations, err := c.Locations(ctx)
	if err != nil {
		return Catalog{}, err
	}
	serverTypes, err := c.ServerTypes(ctx)
	if err != nil {
		return Catalog{}, err
	}
	images, err := c.Images(ctx)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Locations: locations, ServerTypes: serverTypes, Images: images}, nil
}
