package cli

import (
	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/provider/digitalocean"
	"github.com/assagman/serverpro/internal/provider/hetzner"
	"github.com/assagman/serverpro/internal/provider/vultr"
)

func (a *app) providerRegistry() *compute.Registry {
	if a.providers != nil {
		return a.providers
	}
	registry := compute.NewRegistry()
	_ = registry.Register(digitalocean.NewComputeProvider(nil))
	_ = registry.Register(hetzner.NewComputeProvider(nil))
	_ = registry.Register(vultr.NewComputeProvider(nil))
	a.providers = registry
	return registry
}
