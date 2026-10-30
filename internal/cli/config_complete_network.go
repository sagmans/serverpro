package cli

import (
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
)

func (a *app) completeNetworkConfig(cfg *config.Config, ask bool) error {
	if err := a.promptDefaultWhen(ask, "egress mode (restricted/open)", &cfg.Network.Egress.Mode); err != nil {
		return err
	}
	switch cfg.Network.Egress.Mode {
	case config.EgressModeRestricted, config.EgressModeOpen:
		return nil
	default:
		return fmt.Errorf("network.egress.mode must be restricted or open")
	}
}
