package lifecycle

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
)

type PlanAction struct {
	Step    string `json:"step"`
	Target  string `json:"target"`
	Details string `json:"details"`
}

type Plan struct {
	Actions []PlanAction `json:"actions"`
}

func BuildPlan(cfg config.Config) Plan {
	actions := []PlanAction{
		{"ensure", "tailscale policy", "tag ownership and Tailscale SSH rule for managed access"},
		{"create", "access policy", "deny public ingress; attach at server creation"},
		{"render", "cloud-init", "admin user, SSH hardening, UFW, updates, AppArmor, journald"},
		{"create", "compute server", fmt.Sprintf("size=%s image=%s location=%s", cfg.Compute.Size, cfg.Compute.Image, cfg.Compute.Location)},
		{"bootstrap", "tailscale", "create one-off auth key; join tailnet and enable Tailscale SSH"},
	}
	if cloudflareTunnelConfigured(cfg) {
		actions = append(actions, PlanAction{"install", "cloudflare tunnel", "connector-only; no public hostname route by default"})
	}
	actions = append(actions,
		PlanAction{"install", "server tools", bootstraptools.DefaultToolsetDescription() + "; Pi and gh authentication remain operator-owned"},
		PlanAction{"lockdown", "egress", cfg.Network.Egress.Mode + " best-effort policy"},
		PlanAction{"validate", "doctor", "hardening, ingress, Tailscale SSH, tunnel, secret checks"},
	)
	return Plan{Actions: actions}
}

func (p Plan) Write(w io.Writer, _ bool) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
