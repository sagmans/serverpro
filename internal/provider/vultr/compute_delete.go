package vultr

import (
	"context"
	"net/http"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
)

func (p ComputeProvider) Delete(ctx context.Context, request compute.DeleteServerRequest) compute.Diagnostics {
	if request.Account.Token == "" {
		return compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	firewallGroupID, hasFirewall := firewallGroupID(request.Record)
	if err := validateMutationRequest(request.Record, p.Name()); err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	client := p.newClient(request.Account.Token)
	id, err := instanceID(request.Record)
	if err != nil {
		if !hasFirewall {
			return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
		}
		return deleteAccessPolicy(ctx, client, request.Account.Token, request.Record, firewallGroupID)
	}
	current, err := client.GetInstance(ctx, id)
	instanceAlreadyDeleted := httpjson.IsStatus(err, http.StatusNotFound)
	if err != nil && !instanceAlreadyDeleted {
		return failure(request.Account.Token, "provider ownership check failed", err)
	}
	if !instanceAlreadyDeleted {
		if err := validateLiveInstanceOwnership(request.Record, current); err != nil {
			return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
		}
		if err := client.DeleteInstance(ctx, id); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return failure(request.Account.Token, "provider instance delete failed", err)
		}
	}
	if hasFirewall {
		return deleteAccessPolicy(ctx, client, request.Account.Token, request.Record, firewallGroupID)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider instance deleted"}}
}

func deleteAccessPolicy(ctx context.Context, client Client, secret string, record compute.ServerRecord, id string) compute.Diagnostics {
	fw, err := client.GetFirewallGroup(ctx, id)
	if httpjson.IsStatus(err, http.StatusNotFound) {
		return compute.Diagnostics{{Status: compute.Pass, Message: "provider instance deleted"}}
	}
	if err != nil {
		return failure(secret, "provider access policy ownership check failed", err)
	}
	if err := validateLiveFirewallGroupOwnership(record, fw); err != nil {
		return compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	if err := client.DeleteFirewallGroup(ctx, id); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
		return failure(secret, "provider access policy delete failed", err)
	}
	return compute.Diagnostics{{Status: compute.Pass, Message: "provider instance deleted"}}
}
