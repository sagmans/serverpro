package lifecycle

import (
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

type provisionStateSaver func(string, state.State) error

type Options struct {
	Config            config.Config
	ComputeAccount    compute.Account
	Creds             credentials.Set
	StatePath         string
	AdminPasswordHash string
	Clients           Clients
	Now               func() time.Time
	saveState         provisionStateSaver
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

func (o Options) stateSaver() provisionStateSaver {
	if o.saveState != nil {
		return o.saveState
	}
	return saveProvisionState
}
