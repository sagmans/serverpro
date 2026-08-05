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

func (c Client) DeleteServerproAuthKeys(ctx context.Context, tags []string) (int, error) {
	if len(tags) == 0 {
		return 0, nil
	}
	keys, err := c.AuthKeys(ctx)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, key := range keys {
		if key.Description != "serverpro bootstrap" || !authKeyHasAnyTag(key, tags) {
			continue
		}
		if err := c.DeleteAuthKey(ctx, key.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func authKeyHasAnyTag(key AuthKey, tags []string) bool {
	keyTags := key.Capabilities.Devices.Create.Tags
	for _, tag := range tags {
		if contains(keyTags, tag) {
			return true
		}
	}
	return false
}
