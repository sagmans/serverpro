package digitalocean

import (
	"context"
	"strconv"

	"github.com/sagmans/serverpro/internal/compute"
)

func (p ComputeProvider) Status(ctx context.Context, ref compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	if ref.Account.Token == "" {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "provider credential missing"}}
	}
	id, err := dropletID(ref.Record)
	if err != nil {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: err.Error()}}
	}
	droplet, err := p.newClient(ref.Account.Token).GetDroplet(ctx, id)
	if err != nil {
		return compute.ServerStatus{}, failure(ref.Account.Token, "provider droplet status failed", err)
	}
	return statusFromDroplet(ref.Record, droplet), compute.Diagnostics{{Status: compute.Pass, Message: "provider droplet status loaded"}}
}

func statusFromDroplet(record compute.ServerRecord, droplet Droplet) compute.ServerStatus {
	record.ID = strconv.FormatInt(droplet.ID, 10)
	record.Name = droplet.Name
	record.PublicIPv4 = publicIPv4(droplet)
	record.PublicIPv6 = ""
	if len(droplet.Tags) > 0 {
		record.Labels = tagsToLabels(droplet.Tags)
	}
	return compute.ServerStatus{Record: record, Power: droplet.Status, PublicIPv4: record.PublicIPv4}
}
