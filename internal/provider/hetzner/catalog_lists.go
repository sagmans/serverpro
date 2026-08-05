package hetzner

import (
	"context"
	"fmt"
	"net/http"
)

func (c Client) Locations(ctx context.Context) ([]Location, error) {
	return pagedList(func(page int) ([]Location, *int, error) {
		var res struct {
			Locations []Location `json:"locations"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/locations?per_page=50&page=%d", page), nil, &res)
		return res.Locations, res.Meta.Pagination.NextPage, err
	})
}

func (c Client) ServerTypes(ctx context.Context) ([]ServerType, error) {
	return pagedList(func(page int) ([]ServerType, *int, error) {
		var res struct {
			ServerTypes []ServerType `json:"server_types"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/server_types?per_page=50&page=%d", page), nil, &res)
		return res.ServerTypes, res.Meta.Pagination.NextPage, err
	})
}

func (c Client) Images(ctx context.Context) ([]Image, error) {
	return pagedList(func(page int) ([]Image, *int, error) {
		var res struct {
			Images []Image `json:"images"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/images?type=system&status=available&per_page=50&page=%d", page), nil, &res)
		return res.Images, res.Meta.Pagination.NextPage, err
	})
}

func pagedList[T any](fetch func(page int) ([]T, *int, error)) ([]T, error) {
	var out []T
	page := 1
	for {
		items, nextPage, err := fetch(page)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if nextPage == nil {
			return out, nil
		}
		page = *nextPage
	}
}
