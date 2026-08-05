package vultr

import "context"

func (c Client) Catalog(ctx context.Context) (Catalog, error) {
	regions, err := c.Regions(ctx)
	if err != nil {
		return Catalog{}, err
	}
	plans, err := c.Plans(ctx)
	if err != nil {
		return Catalog{}, err
	}
	osList, err := c.OS(ctx)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Regions: regions, Plans: plans, OS: osList}, nil
}
