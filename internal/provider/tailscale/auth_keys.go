package tailscale

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

func (c Client) CreateAuthKey(ctx context.Context, tags []string, expiry time.Duration) (AuthKey, error) {
	payload := map[string]any{
		"capabilities": map[string]any{"devices": map[string]any{"create": map[string]any{
			"reusable": false, "ephemeral": false, "preauthorized": true, "tags": tags,
		}}},
		"expirySeconds": int(expiry.Seconds()),
		"description":   "serverpro bootstrap",
	}
	var out AuthKey
	err := c.api.Do(ctx, http.MethodPost, "/tailnet/"+url.PathEscape(c.tailnet)+"/keys", payload, &out)
	return out, err
}

func (c Client) AuthKeys(ctx context.Context) ([]AuthKey, error) {
	var out struct {
		Keys []AuthKey `json:"keys"`
	}
	err := c.api.Do(ctx, http.MethodGet, "/tailnet/"+url.PathEscape(c.tailnet)+"/keys", nil, &out)
	return out.Keys, err
}

func (c Client) DeleteAuthKey(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return c.api.Do(ctx, http.MethodDelete, "/tailnet/"+url.PathEscape(c.tailnet)+"/keys/"+url.PathEscape(id), nil, nil)
}
