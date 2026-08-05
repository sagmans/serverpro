package digitalocean

import (
	"context"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
)

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

func (p ComputeProvider) Capabilities(context.Context) compute.Capabilities {
	return compute.Capabilities{CreateServer: true, DeleteServer: true, PowerServer: true, Catalog: true, ListServers: true}
}

func (p ComputeProvider) Doctor(ctx context.Context, account compute.Account) compute.Diagnostics {
	if account.Token == "" {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	if _, err := p.newClient(account.Token).Regions(ctx); err != nil {
		return failure(account.Token, "provider credential validation failed", err)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider credential valid"}}
}

func (p ComputeProvider) Catalog(ctx context.Context, query compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	if query.Account.Token == "" {
		return compute.Catalog{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	catalog, err := p.newClient(query.Account.Token).Catalog(ctx)
	if err != nil {
		return compute.Catalog{}, failure(query.Account.Token, "provider catalog failed", err)
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
	out := make([]compute.ServerRecord, 0, len(droplets))
	managed := false
	for _, droplet := range droplets {
		record := serverRecordFromLive(droplet)
		if _, _, ok := ownership.OwnershipFromLabels(record.Labels); ok {
			managed = true
		}
		out = append(out, record)
	}
	if !managed {
		return out, compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet list loaded"}}
	}
	firewalls, err := client.ListFirewalls(ctx)
	if err != nil {
		return nil, failure(query.Account.Token, "provider access policy list failed", err)
	}
	for index := range out {
		if _, _, ok := ownership.OwnershipFromLabels(out[index].Labels); !ok {
			continue
		}
		policyID, err := recoverFirewallID(out[index], firewalls)
		if err != nil {
			return nil, failure(query.Account.Token, "provider access policy recovery failed", err)
		}
		out[index].ManagedResources = []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: policyID}}
	}
	return out, compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet list loaded"}}
}
