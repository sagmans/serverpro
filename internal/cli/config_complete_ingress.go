package cli

import "github.com/sagmans/serverpro/internal/config"

func (a *app) completeIngressConfig(cfg *config.Config, ask bool) error {
	if cfg.Network.Ingress == "" {
		cfg.Network.Ingress = "none"
	}
	if !ask || (!a.interactiveTerminal() && a.selectChoice == nil) {
		applyIngressSelection(cfg, cfg.Network.Ingress)
		return nil
	}
	selected, err := a.promptChoice("public ingress", cfg.Network.Ingress, []choice{
		{Value: "none", Description: "no public ingress; Tailscale SSH only"},
		{Value: "cloudflare-tunnel", Description: "Cloudflare Tunnel without opening provider firewall ports"},
	})
	if err != nil {
		return err
	}
	applyIngressSelection(cfg, selected)
	return nil
}

func applyIngressSelection(cfg *config.Config, selected string) {
	cfg.Network.Ingress = selected
	cfg.Cloudflare.Tunnel.Enabled = selected == "cloudflare-tunnel"
	cfg.Cloudflare.Tunnel.CreateConnectorOnly = selected == "cloudflare-tunnel"
	if selected != "cloudflare-tunnel" {
		cfg.Cloudflare.Tunnel.SmokeRoute = config.SmokeRoute{}
	}
}
