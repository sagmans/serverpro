package vultr

import (
	"context"
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/redact"
)

const supportedImageReference = "2284"

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
	instances, err := client.ListInstances(ctx)
	if err != nil {
		return nil, failure(query.Account.Token, "provider instance list failed", err)
	}
	out := make([]compute.ServerRecord, 0, len(instances))
	for _, inst := range instances {
		record := serverRecordFromLive(inst)
		if _, _, managed := ownership.OwnershipFromLabels(record.Labels); managed {
			if err := validateRecoveryMetadata(record); err != nil {
				return nil, failure(query.Account.Token, "provider recovery inventory invalid", err)
			}
			firewallID, ok := firewallGroupID(record)
			if !ok {
				return nil, failure(query.Account.Token, "provider recovery inventory invalid", fmt.Errorf("provider managed access policy missing for %q", record.Name))
			}
			firewall, err := client.GetFirewallGroup(ctx, firewallID)
			if err != nil {
				return nil, failure(query.Account.Token, "provider recovery inventory invalid", err)
			}
			if err := validateLiveFirewallGroupOwnership(record, firewall); err != nil {
				return nil, failure(query.Account.Token, "provider recovery inventory invalid", err)
			}
		}
		out = append(out, record)
	}
	return out, compute.Diagnostics{{Status: compute.Pass, Message: "provider instance list loaded"}}
}

func validateRecoveryMetadata(record compute.ServerRecord) error {
	var missing []string
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "id", value: record.ID},
		{name: "name", value: record.Name},
		{name: "region", value: record.Location},
		{name: "plan", value: record.Size},
		{name: "os", value: record.Image},
	} {
		if field.value == "" || field.value == "0" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("provider instance recovery metadata missing: %s", strings.Join(missing, ", "))
	}
	return nil
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
	record.Labels = tagsToLabels(inst.Tags)
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
		record.ProviderState = map[string]string{"firewall_group_id": inst.FirewallGroupID}
	}
	return record
}
