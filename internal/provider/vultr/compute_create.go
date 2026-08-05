package vultr

import (
	"context"
	"fmt"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
)

func (p ComputeProvider) Create(ctx context.Context, request compute.CreateServerRequest) (compute.ServerRecord, compute.Diagnostics) {
	if request.Account.Token == "" {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	client := p.newClient(request.Account.Token)
	osID, err := osIDFromImage(request.Intent.Image)
	if err != nil {
		return compute.ServerRecord{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	firewallGroupID, err := ensureFirewallGroup(ctx, client, request)
	if err != nil {
		record := compute.ServerRecord{}
		if firewallGroupID != "" {
			record = partialServerRecordFromCreateRequest(request, firewallGroupID)
		}
		return record, failure(request.Account.Token, "provider access policy create failed", err)
	}
	inst, err := client.CreateInstance(ctx, createInstanceInputFromRequest(request, osID, firewallGroupID))
	if err != nil {
		record := partialServerRecordFromCreateRequest(request, firewallGroupID)
		record = reconcileCreatedInstance(ctx, client, record)
		return record, failure(request.Account.Token, "provider instance create failed", err, bootstrapRedactionSecrets(request.BootstrapData)...)
	}
	return serverRecordFromCreateRequest(request, inst, firewallGroupID), compute.Diagnostics{{Status: compute.Pass, Message: "provider instance created"}}
}

func ensureFirewallGroup(ctx context.Context, client Client, request compute.CreateServerRequest) (string, error) {
	if raw, ok := compute.ManagedResourceID(request.ManagedResources, compute.ManagedResourceAccessPolicy); ok {
		return raw, validateCheckpointFirewallGroup(ctx, client, request, raw)
	}
	if raw := request.ProviderState["firewall_group_id"]; raw != "" {
		return raw, validateCheckpointFirewallGroup(ctx, client, request, raw)
	}
	fw, err := client.CreateFirewallGroup(ctx, firewallGroupName(request.Intent.Name))
	if err != nil {
		return "", err
	}
	if err := ensureFirewallRules(ctx, client, fw.ID); err != nil {
		return fw.ID, err
	}
	return fw.ID, nil
}

func validateCheckpointFirewallGroup(ctx context.Context, client Client, request compute.CreateServerRequest, groupID string) error {
	fw, err := client.GetFirewallGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if fw.ID != groupID {
		return fmt.Errorf("provider ownership mismatch: live firewall group id %q state %q", fw.ID, groupID)
	}
	if err := validateLiveFirewallGroupOwnership(partialServerRecordFromCreateRequest(request, groupID), fw); err != nil {
		return err
	}
	if fw.InstanceCount != 0 {
		return fmt.Errorf("provider ownership mismatch: live firewall group has instance attachment")
	}
	return ensureFirewallRules(ctx, client, groupID)
}

func ensureFirewallRules(ctx context.Context, client Client, groupID string) error {
	existing, err := client.ListFirewallRules(ctx, groupID)
	if err != nil {
		return err
	}
	requiredRules := requiredFirewallRules()
	for _, live := range existing {
		if !firewallRuleIsRequired(live, requiredRules) {
			return fmt.Errorf("provider access policy has unexpected rule")
		}
	}
	for _, required := range requiredRules {
		found := false
		for _, live := range existing {
			if firewallRuleMatches(live, required) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if _, err := client.CreateFirewallRule(ctx, groupID, required); err != nil {
			return err
		}
	}
	return nil
}

func requiredFirewallRules() []CreateFirewallRuleInput {
	return []CreateFirewallRuleInput{
		{IPType: "v4", Protocol: "udp", Port: "41641", Subnet: "0.0.0.0", SubnetSize: 0, Notes: "tailscale wireguard"},
		{IPType: "v6", Protocol: "udp", Port: "41641", Subnet: "::", SubnetSize: 0, Notes: "tailscale wireguard"},
		{IPType: "v4", Protocol: "udp", Port: "3478", Subnet: "0.0.0.0", SubnetSize: 0, Notes: "tailscale stun"},
		{IPType: "v6", Protocol: "udp", Port: "3478", Subnet: "::", SubnetSize: 0, Notes: "tailscale stun"},
	}
}

func firewallRuleIsRequired(live FirewallRule, requiredRules []CreateFirewallRuleInput) bool {
	for _, required := range requiredRules {
		if firewallRuleMatches(live, required) {
			return true
		}
	}
	return false
}

func firewallRuleMatches(live FirewallRule, required CreateFirewallRuleInput) bool {
	return live.Action == "accept" &&
		live.IPType == required.IPType &&
		live.Protocol == required.Protocol &&
		live.Port == required.Port &&
		live.Subnet == required.Subnet &&
		live.SubnetSize == required.SubnetSize &&
		live.Source == ""
}

func createInstanceInputFromRequest(request compute.CreateServerRequest, osID int64, firewallGroupID string) CreateInstanceInput {
	labels := ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels)
	return CreateInstanceInput{
		Region:          request.Intent.Location,
		Plan:            request.Intent.Size,
		OSID:            osID,
		Label:           request.Intent.Name,
		Hostname:        request.Intent.Name,
		Tags:            labelsToTags(labels),
		FirewallGroupID: firewallGroupID,
		UserData:        request.BootstrapData,
	}
}

func serverRecordFromCreateRequest(request compute.CreateServerRequest, inst Instance, firewallGroupID string) compute.ServerRecord {
	record := partialServerRecordFromCreateRequest(request, firewallGroupID)
	record.ID = inst.ID
	record.Name = inst.Label
	record.PublicIPv4 = inst.MainIP
	return record
}

func partialServerRecordFromCreateRequest(request compute.CreateServerRequest, firewallGroupID string) compute.ServerRecord {
	return compute.ServerRecord{
		Provider:         compute.ProviderName("vultr"),
		Namespace:        request.Intent.Namespace,
		Server:           request.Intent.Server,
		Name:             request.Intent.Name,
		Location:         request.Intent.Location,
		Size:             request.Intent.Size,
		Image:            request.Intent.Image,
		Labels:           ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels),
		ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: firewallGroupID}},
	}
}

func reconcileCreatedInstance(ctx context.Context, client Client, record compute.ServerRecord) compute.ServerRecord {
	inst, err := client.FindInstanceByLabel(ctx, record.Name)
	if err != nil {
		return record
	}
	if err := validateLiveInstanceOwnership(record, inst); err != nil {
		return record
	}
	record.ID = inst.ID
	record.Name = inst.Label
	record.PublicIPv4 = inst.MainIP
	if len(inst.Tags) > 0 {
		record.Labels = tagsToLabels(inst.Tags)
	}
	return record
}
