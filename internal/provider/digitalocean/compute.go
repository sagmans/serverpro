package digitalocean

import (
	"context"
	"fmt"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/redact"
)

const supportedImageReference = "ubuntu-24-04-x64"

type ClientFactory func(token string) Client

type ComputeProvider struct {
	newClient ClientFactory
}

func NewComputeProvider(factory ClientFactory) ComputeProvider {
	if factory == nil {
		factory = New
	}
	return ComputeProvider{newClient: factory}
}

func (p ComputeProvider) Name() compute.ProviderName { return compute.ProviderName("digitalocean") }

// SupportsImageReference keeps offline validation aligned with Noble-specific bootstrap.
func (ComputeProvider) SupportsImageReference(image string) bool {
	return image == supportedImageReference
}

func (p ComputeProvider) Capabilities(context.Context) compute.Capabilities {
	return compute.Capabilities{CreateServer: true, DeleteServer: true, PowerServer: true, Catalog: true, ListServers: true}
}

func (p ComputeProvider) Doctor(ctx context.Context, account compute.Account) compute.Diagnostics {
	if account.Token == "" {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	if _, err := p.newClient(account.Token).Regions(ctx); err != nil {
		message := redact.New(account.Token).String(fmt.Sprintf("provider credential validation failed: %v", err))
		return compute.Diagnostics{{Status: compute.Fail, Message: message}}
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider credential valid"}}
}

func (p ComputeProvider) Catalog(ctx context.Context, query compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	if query.Account.Token == "" {
		return compute.Catalog{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	catalog, err := p.newClient(query.Account.Token).Catalog(ctx)
	if err != nil {
		message := redact.New(query.Account.Token).String(fmt.Sprintf("provider catalog failed: %v", err))
		return compute.Catalog{}, compute.Diagnostics{{Status: compute.Fail, Message: message}}
	}
	return mapCatalog(catalog, query.Location), compute.Diagnostics{{Status: compute.Pass, Message: "provider catalog loaded"}}
}

func (p ComputeProvider) List(ctx context.Context, query compute.ListServersQuery) ([]compute.ServerRecord, compute.Diagnostics) {
	if query.Account.Token == "" {
		return nil, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	client := p.newClient(query.Account.Token)
	droplets, err := client.ListDroplets(ctx)
	if err != nil {
		return nil, failure(query.Account.Token, "provider droplet list failed", err)
	}
	firewalls, err := client.ListFirewalls(ctx)
	if err != nil {
		return nil, failure(query.Account.Token, "provider firewall list failed", err)
	}
	records, err := recoverServerRecords(droplets, firewalls)
	if err != nil {
		return nil, failure(query.Account.Token, "provider recovery inventory invalid", err)
	}
	return records, compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet list loaded"}}
}
