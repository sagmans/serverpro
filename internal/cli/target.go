package cli

import (
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/servername"
	"github.com/sagmans/serverpro/internal/state"
)

func targetServer(server string) string {
	return servername.Normalize(server)
}

func validateStateTarget(cfg config.Config, st state.State) error {
	return state.ValidateTarget(state.Target{Namespace: cfg.Project, Server: cfg.Server, ComputeServerName: cfg.Compute.Name, CloudflareTunnelName: cfg.Cloudflare.Tunnel.Name}, st)
}
