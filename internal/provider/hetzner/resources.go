package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

func (c Client) CreateFirewall(ctx context.Context, name string, labels map[string]string) (Firewall, error) {
	payload := map[string]any{"name": name, "labels": labels, "rules": []any{}}
	var res struct {
		Firewall Firewall `json:"firewall"`
	}
	return res.Firewall, c.api.Do(ctx, http.MethodPost, "/firewalls", payload, &res)
}

type CreateServerInput struct {
	Name     string
	Location string
	Size     string
	Image    string
	Labels   map[string]string
}

func (c Client) CreateServer(ctx context.Context, input CreateServerInput, firewallID int64, userData string) (Server, int64, error) {
	payload := map[string]any{
		"name":               input.Name,
		"server_type":        input.Size,
		"image":              input.Image,
		"location":           input.Location,
		"labels":             input.Labels,
		"user_data":          userData,
		"firewalls":          []map[string]int64{{"firewall": firewallID}},
		"public_net":         map[string]bool{"enable_ipv4": true, "enable_ipv6": false},
		"start_after_create": true,
	}
	var res struct {
		Server Server `json:"server"`
		Action Action `json:"action"`
	}
	if err := c.api.Do(ctx, http.MethodPost, "/servers", payload, &res); err != nil {
		return Server{}, 0, err
	}
	return res.Server, res.Action.ID, nil
}

func (c Client) GetServer(ctx context.Context, id int64) (Server, error) {
	var res struct {
		Server Server `json:"server"`
	}
	return res.Server, c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/servers/%d", id), nil, &res)
}

func (c Client) FindServerByName(ctx context.Context, name string) (Server, error) {
	var res struct {
		Servers []Server `json:"servers"`
	}
	if err := c.api.Do(ctx, http.MethodGet, "/servers?name="+url.QueryEscape(name), nil, &res); err != nil {
		return Server{}, err
	}
	if len(res.Servers) == 0 {
		return Server{}, fmt.Errorf("provider server %q not found", name)
	}
	if len(res.Servers) > 1 {
		return Server{}, fmt.Errorf("provider server name %q is ambiguous", name)
	}
	return res.Servers[0], nil
}

// ListServers pages the full account inventory so import can rebuild local state.
func (c Client) ListServers(ctx context.Context) ([]Server, error) {
	return pagedList(func(page int) ([]Server, *int, error) {
		var res struct {
			Servers []Server `json:"servers"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/servers?per_page=50&page=%d", page), nil, &res)
		return res.Servers, res.Meta.Pagination.NextPage, err
	})
}

func (c Client) DeleteServer(ctx context.Context, id int64) (int64, error) {
	var res struct {
		Action Action `json:"action"`
	}
	if err := c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/servers/%d", id), nil, &res); err != nil {
		return 0, err
	}
	return res.Action.ID, nil
}

func (c Client) PowerOnServer(ctx context.Context, id int64) (int64, error) {
	return c.serverAction(ctx, id, "poweron")
}

func (c Client) ShutdownServer(ctx context.Context, id int64) (int64, error) {
	return c.serverAction(ctx, id, "shutdown")
}

func (c Client) RebootServer(ctx context.Context, id int64) (int64, error) {
	return c.serverAction(ctx, id, "reboot")
}

func (c Client) serverAction(ctx context.Context, id int64, action string) (int64, error) {
	var res struct {
		Action Action `json:"action"`
	}
	if err := c.api.Do(ctx, http.MethodPost, fmt.Sprintf("/servers/%d/actions/%s", id, action), nil, &res); err != nil {
		return 0, err
	}
	return res.Action.ID, nil
}

func (c Client) GetFirewall(ctx context.Context, id int64) (Firewall, error) {
	var res struct {
		Firewall Firewall `json:"firewall"`
	}
	return res.Firewall, c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/firewalls/%d", id), nil, &res)
}

// ListFirewalls retrieves ownership-labeled access policies needed for safe import.
func (c Client) ListFirewalls(ctx context.Context) ([]Firewall, error) {
	return pagedList(func(page int) ([]Firewall, *int, error) {
		var res struct {
			Firewalls []Firewall `json:"firewalls"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/firewalls?per_page=50&page=%d", page), nil, &res)
		return res.Firewalls, res.Meta.Pagination.NextPage, err
	})
}

func (c Client) DeleteFirewall(ctx context.Context, id int64) error {
	return c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/firewalls/%d", id), nil, nil)
}
