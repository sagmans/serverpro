package digitalocean

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
)

func (p ComputeProvider) Delete(ctx context.Context, request compute.DeleteServerRequest) compute.Diagnostics {
	if request.Account.Token == "" {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	accessPolicyID, hasFirewall := firewallID(request.Record)
	if err := validateMutationRequest(request.Record, p.Name()); err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	client := p.newClient(request.Account.Token)
	id, idErr := dropletID(request.Record)
	if idErr != nil && !hasFirewall {
		return compute.Diagnostics{{Status: compute.Fail, Message: idErr.Error()}}
	}

	var current Droplet
	dropletExists := false
	if idErr == nil {
		var err error
		current, err = client.GetDroplet(ctx, id)
		if err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return failure(request.Account.Token, "provider ownership check failed", err)
		}
		dropletExists = err == nil
		if dropletExists {
			if err := validateLiveDropletOwnership(request.Record, current); err != nil {
				return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
			}
		}
	}

	// Validate every tracked resource before the first DELETE so a rejected
	// access policy cannot leave a partially deleted provider graph.
	var firewall Firewall
	firewallExists := false
	if hasFirewall {
		var err error
		firewall, err = client.GetFirewall(ctx, accessPolicyID)
		if err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return failure(request.Account.Token, "provider access policy ownership check failed", err)
		}
		firewallExists = err == nil
		if firewallExists {
			if err := validateLiveFirewallOwnership(request.Record, firewall); err != nil {
				if !legacyFirewallTargetsServer(request.Record, firewall) {
					return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
				}
				// Historical ownership tags attach a firewall to every matching
				// Droplet, so live inventory must bound their deletion impact.
				droplets, listErr := client.ListDroplets(ctx)
				if listErr != nil {
					return failure(request.Account.Token, "provider legacy access policy impact check failed", listErr)
				}
				if impactErr := validateLegacyFirewallImpact(request.Record, firewall, dropletExists, droplets); impactErr != nil {
					return compute.Diagnostics{{Status: compute.Fail, Message: impactErr.Error()}}
				}
			}
		}
	}

	if dropletExists {
		if err := client.DeleteDroplet(ctx, id); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return failure(request.Account.Token, "provider droplet delete failed", err)
		}
	}
	if firewallExists {
		if err := client.DeleteFirewall(ctx, accessPolicyID); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return failure(request.Account.Token, "provider access policy delete failed", err)
		}
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet deleted"}}
}

func legacyFirewallTargetsServer(record compute.ServerRecord, firewall Firewall) bool {
	if record.Name == "" || firewall.Name != firewallName(record.Name) || len(firewall.DropletIDs) != 0 {
		return false
	}
	expected := labelsToTags(ownership.ProviderLabels(record.Namespace, record.Server, record.Labels))
	return multisetsMatch(firewall.Tags, expected)
}

func validateLegacyFirewallImpact(record compute.ServerRecord, firewall Firewall, dropletExists bool, droplets []Droplet) error {
	targetID, targetIDErr := dropletID(record)
	selectors := make(map[string]bool, len(firewall.Tags))
	for _, tag := range firewall.Tags {
		selectors[tag] = true
	}
	for _, droplet := range droplets {
		matched := false
		for _, tag := range droplet.Tags {
			if selectors[tag] {
				matched = true
				break
			}
		}
		if matched && (!dropletExists || targetIDErr != nil || droplet.ID != targetID) {
			return fmt.Errorf("provider ownership mismatch: legacy firewall selector matches unrelated live droplet")
		}
	}
	return nil
}
