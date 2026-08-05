package vultr

import (
	"context"
	"fmt"

	"github.com/sagmans/serverpro/internal/compute"
)

func (p ComputeProvider) Power(ctx context.Context, request compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	if request.Account.Token == "" {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	id, err := instanceID(request.Record)
	if err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	if err := validateMutationRequest(request.Record, p.Name()); err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	client := p.newClient(request.Account.Token)
	current, err := client.GetInstance(ctx, id)
	if err != nil {
		return compute.ServerStatus{}, failure(request.Account.Token, "provider ownership check failed", err)
	}
	if err := validateLiveInstanceOwnership(request.Record, current); err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	switch request.Action {
	case compute.PowerStart:
		err = client.StartInstance(ctx, id)
	case compute.PowerStop:
		err = client.HaltInstance(ctx, id)
	case compute.PowerRestart:
		err = client.RebootInstance(ctx, id)
	default:
		err = fmt.Errorf("unsupported power action %q", request.Action)
	}
	if err != nil {
		return compute.ServerStatus{}, failure(request.Account.Token, "provider power action failed", err)
	}
	return p.Status(ctx, compute.ServerRef{Account: request.Account, Record: request.Record})
}
