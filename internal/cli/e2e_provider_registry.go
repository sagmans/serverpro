//go:build serverpro_e2e

package cli

import (
	"net/http"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/provider/digitalocean"
	"github.com/sagmans/serverpro/internal/provider/hetzner"
	"github.com/sagmans/serverpro/internal/provider/vultr"
)

const e2eProviderHTTPTimeout = 5 * time.Second

func e2eProviderRegistry(apiURL string) *compute.Registry {
	client := &http.Client{Timeout: e2eProviderHTTPTimeout}
	registry := compute.NewRegistry()
	_ = registry.Register(hetzner.NewComputeProvider(func(token string) hetzner.Client {
		return hetzner.NewWithHTTP(token, apiURL+"/hetzner", client)
	}))
	_ = registry.Register(vultr.NewComputeProvider(func(token string) vultr.Client {
		return vultr.NewWithHTTP(token, apiURL+"/vultr", client)
	}))
	_ = registry.Register(digitalocean.NewComputeProvider(func(token string) digitalocean.Client {
		return digitalocean.NewWithHTTP(token, apiURL+"/digitalocean", client)
	}))
	return registry
}
