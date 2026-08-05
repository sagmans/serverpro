package vultr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type CreateInstanceInput struct {
	Region          string
	Plan            string
	OSID            int64
	Label           string
	Hostname        string
	Tags            []string
	FirewallGroupID string
	UserData        string
}

type CreateFirewallRuleInput struct {
	IPType     string
	Protocol   string
	Port       string
	Subnet     string
	SubnetSize int
	Notes      string
}

type instanceResponse struct {
	Instance Instance `json:"instance"`
}

type instancesResponse struct {
	Instances []Instance `json:"instances"`
	Meta      pageMeta   `json:"meta"`
}

type firewallGroupResponse struct {
	FirewallGroup FirewallGroup `json:"firewall_group"`
}

type firewallRuleResponse struct {
	FirewallRule FirewallRule `json:"firewall_rule"`
}

func (c Client) CreateFirewallGroup(ctx context.Context, name string) (FirewallGroup, error) {
	payload := map[string]any{"description": name}
	var res firewallGroupResponse
	return res.FirewallGroup, c.api.Do(ctx, http.MethodPost, "/firewalls", payload, &res)
}

func (c Client) GetFirewallGroup(ctx context.Context, id string) (FirewallGroup, error) {
	var res firewallGroupResponse
	return res.FirewallGroup, c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/firewalls/%s", id), nil, &res)
}

func (c Client) DeleteFirewallGroup(ctx context.Context, id string) error {
	return c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/firewalls/%s", id), nil, nil)
}

func (c Client) CreateFirewallRule(ctx context.Context, groupID string, input CreateFirewallRuleInput) (FirewallRule, error) {
	payload := map[string]any{
		"ip_type":     input.IPType,
		"protocol":    input.Protocol,
		"subnet":      input.Subnet,
		"subnet_size": input.SubnetSize,
	}
	if input.Port != "" {
		payload["port"] = input.Port
	}
	if input.Notes != "" {
		payload["notes"] = input.Notes
	}
	var res firewallRuleResponse
	return res.FirewallRule, c.api.Do(ctx, http.MethodPost, fmt.Sprintf("/firewalls/%s/rules", groupID), payload, &res)
}

func (c Client) FirewallRules(ctx context.Context, groupID string) ([]FirewallRule, error) {
	return pagedList(func(cursor string) ([]FirewallRule, string, error) {
		var res struct {
			FirewallRules []FirewallRule `json:"firewall_rules"`
			pageMeta
		}
		path := catalogListPath(fmt.Sprintf("/firewalls/%s/rules", groupID), cursor)
		err := c.api.Do(ctx, http.MethodGet, path, nil, &res)
		return res.FirewallRules, res.pageMeta.Meta.Links.Next, err
	})
}

func (c Client) DeleteFirewallRule(ctx context.Context, groupID string, ruleID int) error {
	return c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/firewalls/%s/rules/%d", groupID, ruleID), nil, nil)
}

func (c Client) CreateInstance(ctx context.Context, input CreateInstanceInput) (Instance, error) {
	payload := map[string]any{
		"region":      input.Region,
		"plan":        input.Plan,
		"os_id":       input.OSID,
		"label":       input.Label,
		"hostname":    input.Hostname,
		"tags":        input.Tags,
		"enable_ipv6": false,
	}
	if input.FirewallGroupID != "" {
		payload["firewall_group_id"] = input.FirewallGroupID
	}
	if input.UserData != "" {
		payload["user_data"] = encodeUserData(input.UserData)
	}
	var res instanceResponse
	if err := c.api.Do(ctx, http.MethodPost, "/instances", payload, &res); err != nil {
		return Instance{}, err
	}
	return res.Instance, nil
}

func (c Client) GetInstance(ctx context.Context, id string) (Instance, error) {
	var res instanceResponse
	return res.Instance, c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/instances/%s", id), nil, &res)
}

func (c Client) FindInstanceByLabel(ctx context.Context, label string) (Instance, error) {
	var res instancesResponse
	if err := c.api.Do(ctx, http.MethodGet, "/instances?label="+url.QueryEscape(label), nil, &res); err != nil {
		return Instance{}, err
	}
	matches := filterInstancesByLabel(res.Instances, label)
	if len(matches) == 0 {
		return Instance{}, fmt.Errorf("provider instance %q not found", label)
	}
	if len(matches) > 1 {
		return Instance{}, fmt.Errorf("provider instance label %q is ambiguous", label)
	}
	return matches[0], nil
}

// ListInstances pages account instances so import can rebuild local state from tags.
func (c Client) ListInstances(ctx context.Context) ([]Instance, error) {
	return pagedList(func(cursor string) ([]Instance, string, error) {
		var res struct {
			Instances []Instance `json:"instances"`
			pageMeta
		}
		err := c.api.Do(ctx, http.MethodGet, catalogListPath("/instances", cursor), nil, &res)
		return res.Instances, res.pageMeta.Meta.Links.Next, err
	})
}

func (c Client) DeleteInstance(ctx context.Context, id string) error {
	return c.api.Do(ctx, http.MethodDelete, fmt.Sprintf("/instances/%s", id), nil, nil)
}

func (c Client) StartInstance(ctx context.Context, id string) error {
	return c.instanceAction(ctx, id, "start")
}

func (c Client) HaltInstance(ctx context.Context, id string) error {
	return c.instanceAction(ctx, id, "halt")
}

func (c Client) RebootInstance(ctx context.Context, id string) error {
	return c.instanceAction(ctx, id, "reboot")
}

func (c Client) instanceAction(ctx context.Context, id, action string) error {
	return c.api.Do(ctx, http.MethodPost, fmt.Sprintf("/instances/%s/%s", id, action), nil, nil)
}
