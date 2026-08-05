package lifecycle

import (
	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
)

type Options struct {
	Config            config.Config
	ComputeAccount    compute.Account
	Creds             credentials.Set
	StatePath         string
	AdminPasswordHash string
	Clients           Clients
}
