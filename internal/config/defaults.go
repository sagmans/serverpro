package config

import "github.com/assagman/serverpro/internal/servername"

const defaultCredentialsJSONPath = "~/.config/serverpro/credentials.json"

func DefaultServer() string {
	return servername.Default
}

func Default() Config {
	return Config{
		Server:      servername.Default,
		Credentials: Credentials{Mode: "json", JSONPath: defaultCredentialsJSONPath},
		Compute:     Compute{Location: "fsn1", Size: "cx23", Image: "ubuntu-24.04", Labels: map[string]string{"managed-by": "serverpro"}},
		Admin:       Admin{Username: "deploy"},
		Network:     Network{Ingress: "none", Egress: Egress{Mode: "restricted", PhaseLockdownAfterBootstrap: true, Allow: []string{"dns", "ntp", "ubuntu-security-updates", "tailscale", "cloudflare-tunnel"}}},
		Access:      Access{Tailscale: Tailscale{Enabled: true, SSH: true, Tailnet: "-", Tags: []string{"tag:serverpro-server"}, RootPolicy: "check-or-disabled"}},
		Cloudflare:  Cloudflare{},
		Hardening:   Hardening{Profile: "strict", UnattendedUpgrades: true, AppArmor: true, UFW: true, JournaldPersistent: true},
	}
}
