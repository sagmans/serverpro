package cli

import (
	"strings"

	"github.com/assagman/serverpro/internal/config"
)

func (a *app) completeTailscaleConfig(cfg *config.Config, ask bool) error {
	if err := a.promptDefaultWhen(ask, "Tailscale tailnet", &cfg.Access.Tailscale.Tailnet); err != nil {
		return err
	}
	if len(cfg.Access.Tailscale.Tags) == 0 {
		cfg.Access.Tailscale.Tags = []string{config.ProjectTailscaleTag(cfg.Project)}
	}
	if ask {
		v := strings.Join(cfg.Access.Tailscale.Tags, ",")
		if err := a.promptDefaultWhen(true, "Tailscale tags (comma-separated)", &v); err != nil {
			return err
		}
		cfg.Access.Tailscale.Tags = splitCSV(v)
	}
	return nil
}
