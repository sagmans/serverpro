package digitalocean

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type CreateFirewallInput struct {
	Name string
	Tags []string
}

type CreateDropletInput struct {
	Name     string
	Region   string
	Size     string
	Image    string
	Tags     []string
	UserData string
}

type dropletResponse struct {
	Droplet Droplet `json:"droplet"`
}

type dropletsResponse struct {
	Droplets []Droplet `json:"droplets"`
	pageMeta
}

type firewallResponse struct {
	Firewall Firewall `json:"firewall"`
}

type firewallsResponse struct {
	Firewalls []Firewall `json:"firewalls"`
	pageMeta
}

type actionResponse struct {
	Action Action `json:"action"`
}

func (c Client) EnsureTags(ctx context.Context, tags []string) error {
	seen := map[string]bool{}
	for _, tag := range tags {
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		if err := c.CreateTag(ctx, tag); err != nil {
			if getErr := c.GetTag(ctx, tag); getErr == nil {
				continue
			}
			return err
		}
	}
	return nil
}

func (c Client) CreateTag(ctx context.Context, name string) error {
	return c.api.Do(ctx, http.MethodPost, "/tags", map[string]string{"name": name}, nil)
}

func (c Client) GetTag(ctx context.Context, name string) error {
	return c.api.Do(ctx, http.MethodGet, "/tags/"+url.PathEscape(name), nil, nil)
}

func (c Client) CreateFirewall(ctx context.Context, input CreateFirewallInput) (Firewall, error) {
	payload := map[string]any{
		"name":           input.Name,
		"tags":           input.Tags,
		"inbound_rules":  tailscaleInboundRules(),
		"outbound_rules": allowAllOutboundRules(),
	}
	var res firewallResponse
	return res.Firewall, c.api.Do(ctx, http.MethodPost, "/firewalls", payload, &res)
}

func (c Client) GetFirewall(ctx context.Context, id string) (Firewall, error) {
	var res firewallResponse
	return res.Firewall, c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/firewalls/%s", id), nil, &res)
}

// ListFirewalls retrieves ownership-tagged access policies needed for safe import.
func (c Client) ListFirewalls(ctx context.Context) ([]Firewall, error) {
	return pagedList(func(page int) ([]Firewall, *int, error) {
		var res firewallsResponse
		err := c.api.Do(ctx, http.MethodGet, catalogListPath("/firewalls", page, nil), nil, &res)
		return res.Firewalls, nextPage(res.pageMeta.Links.Pages.Next), err
	})
}

func (c Client) DeleteFirewall(ctx context.Context, id string) error {
	return c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/firewalls/%s", id), nil, nil)
}

func (c Client) DeleteFirewallRules(ctx context.Context, id string, inbound []Rule) error {
	if len(inbound) == 0 {
		return nil
	}
	return c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/firewalls/%s/rules", id), map[string]any{"inbound_rules": inbound}, nil)
}

func (c Client) CreateDroplet(ctx context.Context, input CreateDropletInput) (Droplet, error) {
	payload := map[string]any{
		"name":   input.Name,
		"region": input.Region,
		"size":   input.Size,
		"image":  input.Image,
		"tags":   input.Tags,
		"ipv6":   false,
	}
	if input.UserData != "" {
		payload["user_data"] = input.UserData
	}
	var res dropletResponse
	if err := c.api.Do(ctx, http.MethodPost, "/droplets", payload, &res); err != nil {
		return Droplet{}, err
	}
	return res.Droplet, nil
}

func (c Client) GetDroplet(ctx context.Context, id int64) (Droplet, error) {
	var res dropletResponse
	return res.Droplet, c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/droplets/%d", id), nil, &res)
}

func (c Client) FindDropletByName(ctx context.Context, name string) (Droplet, error) {
	droplets, err := pagedList(func(page int) ([]Droplet, *int, error) {
		var res dropletsResponse
		err := c.api.Do(ctx, http.MethodGet, catalogListPath("/droplets", page, map[string]string{"name": name}), nil, &res)
		return res.Droplets, nextPage(res.pageMeta.Links.Pages.Next), err
	})
	if err != nil {
		return Droplet{}, err
	}
	matches := filterDropletsByName(droplets, name)
	if len(matches) == 0 {
		return Droplet{}, fmt.Errorf("provider droplet %q not found", name)
	}
	if len(matches) > 1 {
		return Droplet{}, fmt.Errorf("provider droplet name %q is ambiguous", name)
	}
	return matches[0], nil
}

// ListDroplets pages account droplets so import can rebuild local state from tags.
func (c Client) ListDroplets(ctx context.Context) ([]Droplet, error) {
	return pagedList(func(page int) ([]Droplet, *int, error) {
		var res dropletsResponse
		err := c.api.Do(ctx, http.MethodGet, catalogListPath("/droplets", page, nil), nil, &res)
		return res.Droplets, nextPage(res.pageMeta.Links.Pages.Next), err
	})
}

func (c Client) DeleteDroplet(ctx context.Context, id int64) error {
	return c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/droplets/%d", id), nil, nil)
}

func (c Client) PowerOnDroplet(ctx context.Context, id int64) error {
	return c.dropletAction(ctx, id, "power_on")
}

func (c Client) ShutdownDroplet(ctx context.Context, id int64) error {
	return c.dropletAction(ctx, id, "shutdown")
}

func (c Client) RebootDroplet(ctx context.Context, id int64) error {
	return c.dropletAction(ctx, id, "reboot")
}

func (c Client) dropletAction(ctx context.Context, id int64, action string) error {
	var res actionResponse
	return c.api.Do(ctx, http.MethodPost, fmt.Sprintf("/droplets/%d/actions", id), map[string]any{"type": action}, &res)
}

func filterDropletsByName(droplets []Droplet, name string) []Droplet {
	var out []Droplet
	for _, droplet := range droplets {
		if droplet.Name == name {
			out = append(out, droplet)
		}
	}
	return out
}
