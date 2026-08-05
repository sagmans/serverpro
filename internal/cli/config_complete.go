package cli

import "github.com/sagmans/serverpro/internal/config"

func (a *app) completeConfig(cfg *config.Config, exists bool) error {
	ask := !exists && !a.nonInteractive
	if err := a.completeConfigIdentity(cfg, ask); err != nil {
		return err
	}
	if err := a.completeComputeConfig(cfg, ask); err != nil {
		return err
	}
	if err := a.completeAdminConfig(cfg, ask); err != nil {
		return err
	}
	if err := a.completeTailscaleConfig(cfg, ask); err != nil {
		return err
	}
	if err := a.completeIngressConfig(cfg, ask); err != nil {
		return err
	}
	if err := a.completeCloudflareConfig(cfg, ask); err != nil {
		return err
	}
	return a.completeNetworkConfig(cfg, ask)
}
