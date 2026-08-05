package digitalocean

import "context"

func (c Client) Catalog(ctx context.Context) (Catalog, error) {
	regions, err := c.Regions(ctx)
	if err != nil {
		return Catalog{}, err
	}
	sizes, err := c.Sizes(ctx)
	if err != nil {
		return Catalog{}, err
	}
	images, err := c.Images(ctx)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Regions: regions, Sizes: sizes, Images: images}, nil
}
