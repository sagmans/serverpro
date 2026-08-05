package digitalocean

import (
	"context"
	"net/http"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/provider/httpjson"
)

func (p ComputeProvider) Delete(ctx context.Context, request compute.DeleteServerRequest) compute.Diagnostics {
	if request.Account.Token == "" {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	accessPolicyID, hasFirewall := firewallID(request.Record)
	if err := validateMutationRequest(request.Account, request.Record, p.Name()); err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	client := p.newClient(request.Account.Token)
	id, err := dropletID(request.Record)
	if err != nil {
		if !hasFirewall {
			return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
		}
		return deleteAccessPolicy(ctx, client, request.Account.Token, request.Record, accessPolicyID)
	}
	current, err := client.GetDroplet(ctx, id)
	dropletAlreadyDeleted := httpjson.IsStatus(err, http.StatusNotFound)
	if err != nil && !dropletAlreadyDeleted {
		return failure(request.Account.Token, "provider ownership check failed", err)
	}
	if !dropletAlreadyDeleted {
		if err := validateLiveDropletOwnership(request.Record, current); err != nil {
			return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
		}
		if err := client.DeleteDroplet(ctx, id); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return failure(request.Account.Token, "provider droplet delete failed", err)
		}
	}
	if hasFirewall {
		return deleteAccessPolicy(ctx, client, request.Account.Token, request.Record, accessPolicyID)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet deleted"}}
}

func deleteAccessPolicy(ctx context.Context, client Client, secret string, record compute.ServerRecord, id string) compute.Diagnostics {
	firewall, err := client.GetFirewall(ctx, id)
	if httpjson.IsStatus(err, http.StatusNotFound) {
		return compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet deleted"}}
	}
	if err != nil {
		return failure(secret, "provider access policy ownership check failed", err)
	}
	if err := validateLiveFirewallOwnership(record, firewall); err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	if err := client.DeleteFirewall(ctx, id); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
		return failure(secret, "provider access policy delete failed", err)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet deleted"}}
}
