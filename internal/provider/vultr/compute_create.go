package vultr

import (
	"context"
	"slices"

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
	if raw := request.ProviderState["firewall_group_id"]; raw != "" {
		if err := reconcileFirewallRules(ctx, client, request, raw); err != nil {
			return raw, err
		}
		return raw, nil
	}
	fw, err := client.CreateFirewallGroup(ctx, firewallGroupName(request.Intent.Name))
	if err != nil {
		return "", err
	}
	if request.CheckpointProviderState != nil {
		if err := request.CheckpointProviderState(map[string]string{"firewall_group_id": fw.ID}); err != nil {
			return fw.ID, err
		}
	}
	if err := ensureFirewallRules(ctx, client, fw.ID, nil); err != nil {
		return fw.ID, err
	}
	return fw.ID, nil
}

func reconcileFirewallRules(ctx context.Context, client Client, request compute.CreateServerRequest, groupID string) error {
	group, err := client.GetFirewallGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if err := validateLiveFirewallGroupOwnership(partialServerRecordFromCreateRequest(request, groupID), group); err != nil {
		return err
	}
	existing, err := client.FirewallRules(ctx, groupID)
	if err != nil {
		return err
	}
	for _, rule := range existing {
		if legacyTailscaleSTUNRuleMatches(rule) {
			if err := client.DeleteFirewallRule(ctx, groupID, rule.ID); err != nil {
				return err
			}
		}
	}
	return ensureFirewallRules(ctx, client, groupID, existing)
}

func ensureFirewallRules(ctx context.Context, client Client, groupID string, existing []FirewallRule) error {
	rules := []CreateFirewallRuleInput{
		{IPType: "v4", Protocol: "udp", Port: "41641", Subnet: "0.0.0.0", SubnetSize: 0, Notes: "tailscale wireguard"},
		{IPType: "v6", Protocol: "udp", Port: "41641", Subnet: "::", SubnetSize: 0, Notes: "tailscale wireguard"},
	}
	for _, rule := range rules {
		if slices.ContainsFunc(existing, func(candidate FirewallRule) bool {
			return firewallRuleMatches(candidate, rule)
		}) {
			continue
		}
		if _, err := client.CreateFirewallRule(ctx, groupID, rule); err != nil {
			return err
		}
	}
	return nil
}

func firewallRuleMatches(existing FirewallRule, required CreateFirewallRuleInput) bool {
	// Notes are intentionally excluded because only packet-filtering semantics
	// determine whether retrying must create a missing rule.
	return existing.Action == "accept" && existing.IPType == required.IPType && existing.Protocol == required.Protocol && existing.Port == required.Port && existing.Subnet == required.Subnet && existing.SubnetSize == required.SubnetSize
}

func legacyTailscaleSTUNRuleMatches(rule FirewallRule) bool {
	const (
		legacyPort = "3478"
		legacyNote = "tailscale stun"
	)
	if rule.Action != "accept" || rule.Protocol != "udp" || rule.Port != legacyPort || rule.SubnetSize != 0 || rule.Source != "" || rule.Notes != legacyNote {
		return false
	}
	return rule.IPType == "v4" && rule.Subnet == "0.0.0.0" || rule.IPType == "v6" && rule.Subnet == "::"
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
		Provider:  compute.ProviderName("vultr"),
		Namespace: request.Intent.Namespace,
		Server:    request.Intent.Server,
		Name:      request.Intent.Name,
		Location:  request.Intent.Location,
		Size:      request.Intent.Size,
		Image:     request.Intent.Image,
		Labels:    ownership.ProviderLabels(request.Intent.Namespace, request.Intent.Server, request.Intent.Labels),
		ProviderState: map[string]string{
			"firewall_group_id": firewallGroupID,
		},
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
