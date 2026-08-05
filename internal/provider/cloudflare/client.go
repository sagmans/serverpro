package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/assagman/serverpro/internal/provider/httpjson"
)

type Client struct {
	api       httpjson.Client
	accountID string
}

func New(token, accountID string) Client {
	return Client{api: httpjson.Client{BaseURL: "https://api.cloudflare.com/client/v4", Token: token}, accountID: accountID}
}

func NewWithHTTP(token, accountID, baseURL string, h *http.Client) Client {
	return Client{api: httpjson.Client{BaseURL: baseURL, Token: token, HTTP: h}, accountID: accountID}
}

type Tunnel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func (c Client) ValidateAccount(ctx context.Context) error {
	var out struct {
		Result []Tunnel `json:"result"`
	}
	if err := c.api.Do(ctx, http.MethodGet, c.path("/cfd_tunnel?per_page=1"), nil, &out); err != nil {
		return fmt.Errorf("cloudflare account/token validation failed: %w", err)
	}
	return nil
}

// ListTunnels pages account tunnels so import can reattach known connector IDs.
func (c Client) ListTunnels(ctx context.Context) ([]Tunnel, error) {
	var out []Tunnel
	page := 1
	for {
		var res struct {
			Result     []Tunnel `json:"result"`
			ResultInfo struct {
				Page       int `json:"page"`
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if err := c.api.Do(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=50&page=%d", c.path("/cfd_tunnel"), page), nil, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Result...)
		if res.ResultInfo.TotalPages == 0 || page >= res.ResultInfo.TotalPages {
			return out, nil
		}
		page++
	}
}

func (c Client) CreateTunnel(ctx context.Context, name string) (Tunnel, error) {
	payload := map[string]any{"name": name, "config_src": "cloudflare"}
	var out struct {
		Result Tunnel `json:"result"`
	}
	err := c.api.Do(ctx, http.MethodPost, c.path("/cfd_tunnel"), payload, &out)
	return out.Result, err
}

func (c Client) TunnelToken(ctx context.Context, id string) (string, error) {
	var out struct {
		Result string `json:"result"`
	}
	err := c.api.Do(ctx, http.MethodGet, c.path("/cfd_tunnel/"+id+"/token"), nil, &out)
	return out.Result, err
}

func (c Client) GetTunnel(ctx context.Context, id string) (Tunnel, error) {
	var out struct {
		Result Tunnel `json:"result"`
	}
	err := c.api.Do(ctx, http.MethodGet, c.path("/cfd_tunnel/"+id), nil, &out)
	return out.Result, err
}

func (c Client) DeleteTunnel(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return c.api.Do(ctx, http.MethodDelete, c.path("/cfd_tunnel/"+id), nil, nil)
}

func (c Client) ConnectorOnline(ctx context.Context, id string) (bool, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Minute)
	}
	for {
		t, err := c.GetTunnel(ctx, id)
		if err != nil {
			return false, err
		}
		if t.Status == "healthy" {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return false, nil
			}
			return false, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (c Client) path(suffix string) string { return fmt.Sprintf("/accounts/%s%s", c.accountID, suffix) }
