package digitalocean

import (
	"context"
	"strconv"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/ownership"
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
	if raw := request.ProviderState["firewall_id"]; raw != "" {
		return raw, nil
	}
	tags := labelsToTags(ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels))
	if err := client.EnsureTags(ctx, tags); err != nil {
		return "", err
	}
	fw, err := client.CreateFirewall(ctx, CreateFirewallInput{Name: firewallName(request.Intent.Name), Tags: tags})
	if err != nil {
		return "", err
	}
	if request.CheckpointProviderState != nil {
		if err := request.CheckpointProviderState(map[string]string{"firewall_id": fw.ID}); err != nil {
			return fw.ID, err
		}
	}
	return fw.ID, nil
}

func createDropletInputFromRequest(request compute.CreateServerRequest) CreateDropletInput {
	return CreateDropletInput{
		Name:     request.Intent.Name,
		Region:   request.Intent.Location,
		Size:     request.Intent.Size,
		Image:    request.Intent.Image,
		Tags:     labelsToTags(ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels)),
		UserData: request.BootstrapData,
	}
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
		Provider:  compute.ProviderName("digitalocean"),
		Account:   request.Account.Name,
		Namespace: request.Intent.Namespace,
		Server:    request.Intent.Server,
		Name:      request.Intent.Name,
		Location:  request.Intent.Location,
		Size:      request.Intent.Size,
		Image:     request.Intent.Image,
		Labels:    ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels),
		ProviderState: map[string]string{
			"firewall_id": firewallID,
		},
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
