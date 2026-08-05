package cli

import (
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
)

type globalDoctorRow struct {
	Status  string `json:"status"`
	Scope   string `json:"scope"`
	Count   int    `json:"count,omitempty"`
	Value   string `json:"value,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

func (a *app) runGlobalDoctor() error {
	if a.doctorFix {
		return fmt.Errorf("--fix is only supported by serverpro server doctor")
	}
	providers := a.providerRegistry().List()
	cfg := config.Default()
	tailscaleSSHEnabled := cfg.Access.Tailscale.Enabled && cfg.Access.Tailscale.SSH
	rows := []globalDoctorRow{
		{Status: "pass", Scope: "providers", Count: len(providers)},
		{Status: "pass", Scope: "default_ingress", Value: cfg.Network.Ingress},
		{Status: "pass", Scope: "tailscale_ssh", Enabled: &tailscaleSSHEnabled},
	}
	return writeJSON(a.stdout, rows)
}
