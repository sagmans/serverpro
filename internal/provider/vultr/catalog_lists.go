package vultr

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

const vultrCatalogPageSize = "50"

func (c Client) Regions(ctx context.Context) ([]Region, error) {
	return pagedList(func(cursor string) ([]Region, string, error) {
		var res struct {
			Regions []Region `json:"regions"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, catalogListPath("/regions", cursor), nil, &res)
		return res.Regions, res.pageMeta.Meta.Links.Next, err
	})
}

func (c Client) Plans(ctx context.Context) ([]Plan, error) {
	return pagedList(func(cursor string) ([]Plan, string, error) {
		var res struct {
			Plans []Plan `json:"plans"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, catalogListPath("/plans", cursor), nil, &res)
		return res.Plans, res.pageMeta.Meta.Links.Next, err
	})
}

func (c Client) OS(ctx context.Context) ([]OS, error) {
	return pagedList(func(cursor string) ([]OS, string, error) {
		var res struct {
			OS []OS `json:"os"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, catalogListPath("/os", cursor), nil, &res)
		return res.OS, res.pageMeta.Meta.Links.Next, err
	})
}

func catalogListPath(path, cursor string) string {
	query := url.Values{"per_page": []string{vultrCatalogPageSize}}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	return path + "?" + query.Encode()
}

func pagedList[T any](fetch func(cursor string) ([]T, string, error)) ([]T, error) {
	var out []T
	seen := map[string]bool{}
	cursor := ""
	for {
		items, nextCursor, err := fetch(cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if nextCursor == "" {
			return out, nil
		}
		// Cursor APIs can repeat tokens during provider-side bugs; stop before a loop.
		if seen[nextCursor] {
			return nil, errors.New("provider catalog pagination repeated cursor")
		}
		seen[nextCursor] = true
		cursor = nextCursor
	}
}
