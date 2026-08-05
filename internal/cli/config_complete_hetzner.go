package cli

import "github.com/sagmans/serverpro/internal/config"

func (a *app) completeComputeConfig(cfg *config.Config, ask bool) error {
	choices := catalogChoiceSet{locations: computeLocationChoices(), sizes: computeSizeChoices(), images: computeImageChoices()}
	if ask {
		if liveChoices, ok, err := a.liveCatalogChoices(""); err != nil {
			return err
		} else if ok {
			choices = mergeCatalogChoices(choices, liveChoices)
		}
	}
	if cfg.Compute.Name == "" {
		cfg.Compute.Name = config.ServerResourceName(cfg.Project, cfg.Server)
	}
	priorAutoTunnelName := cfg.Compute.Name
	if cfg.Cloudflare.Tunnel.Name == "" {
		cfg.Cloudflare.Tunnel.Name = cfg.Compute.Name
	}
	if err := a.promptDefaultWhen(ask, "compute server name", &cfg.Compute.Name); err != nil {
		return err
	}
	if cfg.Cloudflare.Tunnel.Name == "" || cfg.Cloudflare.Tunnel.Name == priorAutoTunnelName {
		cfg.Cloudflare.Tunnel.Name = cfg.Compute.Name
	}
	if cfg.Compute.Location == "" || ask {
		if err := a.promptChoiceWhen(ask, "compute location", &cfg.Compute.Location, choices.locations); err != nil {
			return err
		}
		if ask {
			if liveChoices, ok, err := a.liveCatalogChoices(cfg.Compute.Location); err != nil {
				return err
			} else if ok {
				choices = mergeCatalogChoices(choices, liveChoices)
			}
		}
	}
	if err := a.promptChoiceWhen(ask, "compute size", &cfg.Compute.Size, choices.sizes); err != nil {
		return err
	}
	if err := a.promptChoiceWhen(ask, "compute image", &cfg.Compute.Image, choices.images); err != nil {
		return err
	}
	return nil
}

func mergeCatalogChoices(fallback, live catalogChoiceSet) catalogChoiceSet {
	if len(live.locations) > 0 {
		fallback.locations = live.locations
	}
	if len(live.sizes) > 0 {
		fallback.sizes = live.sizes
	}
	if len(live.images) > 0 {
		fallback.images = live.images
	}
	return fallback
}
