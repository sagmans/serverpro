package vultr

import (
	"context"
	"fmt"

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

func (p ComputeProvider) Name() compute.ProviderName { return compute.ProviderName("vultr") }

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
	instances, err := p.newClient(query.Account.Token).ListInstances(ctx)
	if err != nil {
		return nil, failure(query.Account.Token, "provider instance list failed", err)
	}
	out := make([]compute.ServerRecord, 0, len(instances))
	for _, inst := range instances {
		out = append(out, serverRecordFromLive(inst))
	}
	return out, compute.Diagnostics{{Status: compute.Pass, Message: "provider instance list loaded"}}
}

func (p ComputeProvider) Status(ctx context.Context, ref compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	if ref.Account.Token == "" {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	id, err := instanceID(ref.Record)
	if err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	inst, err := p.newClient(ref.Account.Token).GetInstance(ctx, id)
	if err != nil {
		return compute.ServerStatus{}, failure(ref.Account.Token, "provider instance status failed", err)
	}
	return statusFromInstance(ref.Record, inst), compute.Diagnostics{{Status: compute.Pass, Message: "provider instance status loaded"}}
}

func statusFromInstance(record compute.ServerRecord, inst Instance) compute.ServerStatus {
	record.Name = inst.Label
	record.PublicIPv4 = inst.MainIP
	record.PublicIPv6 = ""
	if len(inst.Tags) > 0 {
		record.Labels = tagsToLabels(inst.Tags)
	}
	return compute.ServerStatus{Record: record, Power: inst.PowerStatus, PublicIPv4: inst.MainIP}
}

func serverRecordFromLive(inst Instance) compute.ServerRecord {
	labels := tagsToLabels(inst.Tags)
	namespace, server, _ := ownership.OwnershipFromLabels(labels)
	record := compute.ServerRecord{
		Provider:   compute.ProviderName("vultr"),
		Namespace:  namespace,
		Server:     server,
		ID:         inst.ID,
		Name:       inst.Label,
		Location:   inst.Region,
		Size:       inst.Plan,
		Image:      fmt.Sprintf("%d", inst.OSID),
		PublicIPv4: inst.MainIP,
		Labels:     labels,
	}
	if inst.FirewallGroupID != "" {
		record.ManagedResources = []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: inst.FirewallGroupID}}
	}
	return record
}
