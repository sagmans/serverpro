package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/poll"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
)

const (
	connectorPollInterval = 5 * time.Second
	connectorPollTimeout  = 2 * time.Minute
)

type Client struct {
	api       httpjson.Client
	accountID string
	wait      poll.WaitFunc
}

func New(token, accountID string) Client {
	return Client{api: httpjson.Client{BaseURL: "https://api.cloudflare.com/client/v4", Token: token}, accountID: accountID}
}

func NewWithHTTP(token, accountID, baseURL string, h *http.Client) Client {
	return Client{api: httpjson.Client{BaseURL: baseURL, Token: token, HTTP: h}, accountID: accountID}
}

type Tunnel = ingress.Tunnel

// TunnelIngressRule and TunnelRemoteConfig let callers name the remote
// configuration types through this package, as they do Tunnel.
type (
	TunnelIngressRule  = ingress.TunnelIngressRule
	TunnelRemoteConfig = ingress.TunnelRemoteConfig
)

// ErrTunnelConfigUnproven means a configuration response did not name the
// tunnel that was asked for. An empty or foreign body must not read as a
// tunnel without routes, because the OpenSSH preflight would then pass.
var ErrTunnelConfigUnproven = errors.New("cloudflare: the tunnel configuration response does not name the tunnel")

// tunnelOriginRequest is the part of a tunnel's origin settings that lets a
// client pick its own destination. It appears on each rule and once for the
// whole configuration.
type tunnelOriginRequest struct {
	BastionMode bool   `json:"bastionMode"`
	ProxyType   string `json:"proxyType"`
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

// TunnelIngress reads the remote configuration of a remotely managed tunnel.
// A route added in the dashboard lives only at Cloudflare and never reaches
// the host's disk, so the OpenSSH preflight has to ask the API for it.
//
// Only a response that names the requested tunnel counts. A tunnel that has
// never had a remote configuration, which is how serverpro creates one,
// answers with a null config and has no routes. cloudflared applies the
// configuration-wide originRequest to every rule as defaults and lets a
// rule's own setting win; here a setting from either place counts, so a
// difference from cloudflared can only make the preflight stricter.
func (c Client) TunnelIngress(ctx context.Context, id string) (ingress.TunnelRemoteConfig, error) {
	var out struct {
		Result *struct {
			TunnelID string `json:"tunnel_id"`
			Config   *struct {
				OriginRequest tunnelOriginRequest `json:"originRequest"`
				WARPRouting   struct {
					Enabled bool `json:"enabled"`
				} `json:"warp-routing"`
				Ingress []struct {
					Service       string              `json:"service"`
					OriginRequest tunnelOriginRequest `json:"originRequest"`
				} `json:"ingress"`
			} `json:"config"`
		} `json:"result"`
	}
	if err := c.api.Do(ctx, http.MethodGet, c.path("/cfd_tunnel/"+id+"/configurations"), nil, &out); err != nil {
		return ingress.TunnelRemoteConfig{}, err
	}
	if out.Result == nil || out.Result.TunnelID != id {
		return ingress.TunnelRemoteConfig{}, ErrTunnelConfigUnproven
	}
	config := out.Result.Config
	if config == nil {
		return ingress.TunnelRemoteConfig{}, nil
	}
	remote := ingress.TunnelRemoteConfig{Rules: make([]ingress.TunnelIngressRule, 0, len(config.Ingress)), WARPRouting: config.WARPRouting.Enabled}
	for _, rule := range config.Ingress {
		proxyType := rule.OriginRequest.ProxyType
		if proxyType == "" {
			proxyType = config.OriginRequest.ProxyType
		}
		remote.Rules = append(remote.Rules, ingress.TunnelIngressRule{Service: rule.Service, BastionMode: rule.OriginRequest.BastionMode || config.OriginRequest.BastionMode, ProxyType: proxyType})
	}
	return remote, nil
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
		deadline = time.Now().Add(connectorPollTimeout)
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
		if err := poll.Wait(ctx, c.wait, connectorPollInterval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return false, nil
			}
			return false, err
		}
	}
}

func (c Client) path(suffix string) string { return fmt.Sprintf("/accounts/%s%s", c.accountID, suffix) }
