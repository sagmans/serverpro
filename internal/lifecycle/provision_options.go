package lifecycle

import (
	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
)

type Options struct {
	Config            config.Config
	ComputeAccount    compute.Account
	Creds             credentials.Set
	StatePath         string
	AdminPasswordHash string
	Clients           Clients
}
