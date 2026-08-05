package digitalocean

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
)

func (p ComputeProvider) Create(ctx context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	if request.Account.Token == "" {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	client := p.newClient(request.Account.Token)
	firewallID, err := ensureFirewall(ctx, client, request)
	if err != nil {
		record := compute.ServerRecord{}
		if firewallID != "" {
			record = partialServerRecordFromCreateRequest(request, firewallID)
		}
		return record, failure(request.Account.Token, "provider access policy create failed", err)
	}
	droplet, err := client.CreateDroplet(ctx, createDropletInputFromRequest(request))
	if err != nil {
		record := reconcileCreatedDroplet(ctx, client, partialServerRecordFromCreateRequest(request, firewallID))
		return record, failure(request.Account.Token, "provider droplet create failed", err, bootstrapRedactionSecrets(request.BootstrapData)...)
	}
	return serverRecordFromCreateRequest(request, droplet, firewallID), compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet created"}}
}

func ensureFirewall(ctx context.Context, client Client, request compute.CreateServerRequest) (string, error) {
	if raw, ok := compute.ManagedResourceID(request.ManagedResources, compute.ManagedResourceAccessPolicy); ok {
		return raw, validateCheckpointFirewall(ctx, client, request, raw)
	}
	if raw := request.ProviderState["firewall_id"]; raw != "" {
		return raw, validateCheckpointFirewall(ctx, client, request, raw)
	}
	targetTags := firewallTargetTags(request.Intent.Namespace, request.Intent.Server)
	if err := client.EnsureTags(ctx, dropletTagsFromRequest(request)); err != nil {
		return "", err
	}
	fw, err := client.CreateFirewall(ctx, CreateFirewallInput{Name: firewallName(request.Intent.Name), Tags: targetTags})
	if err != nil {
		return "", err
	}
	return fw.ID, nil
}

func validateCheckpointFirewall(ctx context.Context, client Client, request compute.CreateServerRequest, firewallID string) error {
	firewall, err := client.GetFirewall(ctx, firewallID)
	if err != nil {
		return err
	}
	if firewall.ID != firewallID {
		return fmt.Errorf("provider ownership mismatch: live firewall id %q state %q", firewall.ID, firewallID)
	}
	if err := validateLiveFirewallOwnership(partialServerRecordFromCreateRequest(request, firewallID), firewall); err != nil {
		return err
	}
	if !firewallRulesMatch(firewall.Inbound, tailscaleInboundRules()) || !firewallRulesMatch(firewall.Outbound, allowAllOutboundRules()) {
		return fmt.Errorf("provider access policy has unexpected rules")
	}
	return nil
}

func createDropletInputFromRequest(request compute.CreateServerRequest) CreateDropletInput {
	return CreateDropletInput{
		Name:     request.Intent.Name,
		Region:   request.Intent.Location,
		Size:     request.Intent.Size,
		Image:    request.Intent.Image,
		Tags:     dropletTagsFromRequest(request),
		UserData: request.BootstrapData,
	}
}

func dropletTagsFromRequest(request compute.CreateServerRequest) []string {
	tags := labelsToTags(ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels))
	return append(tags, firewallTargetTag(request.Intent.Namespace, request.Intent.Server))
}

func serverRecordFromCreateRequest(request compute.CreateServerRequest, droplet Droplet, firewallID string) compute.ServerRecord {
	record := partialServerRecordFromCreateRequest(request, firewallID)
	record.ID = strconv.FormatInt(droplet.ID, 10)
	record.Name = droplet.Name
	record.PublicIPv4 = publicIPv4(droplet)
	return record
}

func partialServerRecordFromCreateRequest(request compute.CreateServerRequest, firewallID string) compute.ServerRecord {
	return compute.ServerRecord{
		Provider:         compute.ProviderName("digitalocean"),
		Account:          request.Account.Name,
		Namespace:        request.Intent.Namespace,
		Server:           request.Intent.Server,
		Name:             request.Intent.Name,
		Location:         request.Intent.Location,
		Size:             request.Intent.Size,
		Image:            request.Intent.Image,
		Labels:           ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels),
		ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: firewallID}},
	}
}

func reconcileCreatedDroplet(ctx context.Context, client Client, record compute.ServerRecord) compute.ServerRecord {
	droplet, err := client.FindDropletByName(ctx, record.Name)
	if err != nil {
		return record
	}
	if err := validateLiveDropletOwnership(record, droplet); err != nil {
		return record
	}
	record.ID = strconv.FormatInt(droplet.ID, 10)
	record.Name = droplet.Name
	record.PublicIPv4 = publicIPv4(droplet)
	if len(droplet.Tags) > 0 {
		record.Labels = tagsToLabels(droplet.Tags)
	}
	return record
}
