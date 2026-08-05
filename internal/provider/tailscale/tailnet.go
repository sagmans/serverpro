package tailscale

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

func (c Client) TailnetID(ctx context.Context) (string, error) {
	var out struct {
		Users []struct {
			Type      string `json:"type"`
			TailnetID string `json:"tailnetId"`
		} `json:"users"`
	}
	path := "/tailnet/" + url.PathEscape(c.tailnet) + "/users?type=member"
	if err := c.api.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	id := ""
	for _, user := range out.Users {
		// Shared users belong to another tailnet, so their IDs cannot identify
		// the tailnet whose resources serverpro is authorized to mutate.
		if user.Type != "member" {
			continue
		}
		if user.TailnetID == "" {
			return "", fmt.Errorf("tailscale tailnet identity missing from users response")
		}
		if id == "" {
			id = user.TailnetID
			continue
		}
		if user.TailnetID != id {
			return "", fmt.Errorf("tailscale tailnet identity is inconsistent")
		}
	}
	if id == "" {
		return "", fmt.Errorf("tailscale tailnet identity missing from users response")
	}
	return id, nil
}
