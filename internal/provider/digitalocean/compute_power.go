package digitalocean

import (
	"context"
	"fmt"

	"github.com/assagman/serverpro/internal/compute"
)

func (p ComputeProvider) Power(ctx context.Context, request compute.PowerRequest) (compute.ServerStatus, compute.Diagnostics) {
	if request.Account.Token == "" {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	id, err := dropletID(request.Record)
	if err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	if err := validateMutationRequest(request.Account, request.Record, p.Name()); err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	client := p.newClient(request.Account.Token)
	current, err := client.GetDroplet(ctx, id)
	if err != nil {
		return compute.ServerStatus{}, failure(request.Account.Token, "provider ownership check failed", err)
	}
	if err := validateLiveDropletOwnership(request.Record, current); err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	switch request.Action {
	case compute.PowerStart:
		err = client.PowerOnDroplet(ctx, id)
	case compute.PowerStop:
		err = client.ShutdownDroplet(ctx, id)
	case compute.PowerRestart:
		err = client.RebootDroplet(ctx, id)
	default:
		err = fmt.Errorf("unsupported power action %q", request.Action)
	}
	if err != nil {
		return compute.ServerStatus{}, failure(request.Account.Token, "provider power action failed", err)
	}
	return p.Status(ctx, compute.ServerRef{Account: request.Account, Record: request.Record})
}
