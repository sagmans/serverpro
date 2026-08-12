package digitalocean

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const digitalOceanCatalogPageSize = "200"

func (c Client) Regions(ctx context.Context) ([]Region, error) {
	return pagedList(func(page int) ([]Region, *int, error) {
		var res struct {
			Regions []Region `json:"regions"`
			pageMeta
		}
		if err := c.api.Do(ctx, http.MethodGet, catalogListPath("/regions", page, nil), nil, &res); err != nil {
			return nil, nil, err
		}
		next, err := nextPage(res.Links.Pages.Next)
		return res.Regions, next, err
	})
}

func (c Client) Sizes(ctx context.Context) ([]Size, error) {
	return pagedList(func(page int) ([]Size, *int, error) {
		var res struct {
			Sizes []Size `json:"sizes"`
			pageMeta
		}
		if err := c.api.Do(ctx, http.MethodGet, catalogListPath("/sizes", page, nil), nil, &res); err != nil {
			return nil, nil, err
		}
		next, err := nextPage(res.Links.Pages.Next)
		return res.Sizes, next, err
	})
}

func (c Client) Images(ctx context.Context) ([]Image, error) {
	return pagedList(func(page int) ([]Image, *int, error) {
		var res struct {
			Images []Image `json:"images"`
			pageMeta
		}
		if err := c.api.Do(ctx, http.MethodGet, catalogListPath("/images", page, map[string]string{"type": "distribution"}), nil, &res); err != nil {
			return nil, nil, err
		}
		next, err := nextPage(res.Links.Pages.Next)
		return res.Images, next, err
	})
}

func catalogListPath(path string, page int, extra map[string]string) string {
	query := url.Values{"page": []string{strconv.Itoa(page)}, "per_page": []string{digitalOceanCatalogPageSize}}
	for key, value := range extra {
		query.Set(key, value)
	}
	return path + "?" + query.Encode()
}

func pagedList[T any](fetch func(page int) ([]T, *int, error)) ([]T, error) {
	var out []T
	seen := map[int]bool{1: true}
	page := 1
	for {
		items, next, err := fetch(page)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == nil {
			return out, nil
		}
		// DigitalOcean returns absolute next links; guard provider bugs before looping.
		if seen[*next] {
			return nil, errors.New("provider catalog pagination repeated page")
		}
		seen[*next] = true
		page = *next
	}
}

func nextPage(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("provider pagination next link %q: %w", raw, err)
	}
	page, err := strconv.Atoi(parsed.Query().Get("page"))
	if err != nil {
		return nil, fmt.Errorf("provider pagination next link %q has invalid page: %w", raw, err)
	}
	if page < 1 {
		return nil, fmt.Errorf("provider pagination next link %q has invalid page %d", raw, page)
	}
	return &page, nil
}
