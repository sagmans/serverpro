package config

import "github.com/sagmans/serverpro/internal/servername"

const (
	defaultCredentialsJSONPath = "~/.config/serverpro/credentials.json"
	// TokenDefaultTailnet is token-relative, so destructive global operations cannot treat it as durable identity.
	TokenDefaultTailnet = "-"
)

func DefaultServer() string {
	return servername.Default
}

func Default() Config {
	return Config{
		Server:      servername.Default,
		Credentials: Credentials{JSONPath: defaultCredentialsJSONPath},
		Compute:     Compute{Location: "fsn1", Size: "cx23", Image: "ubuntu-24.04", Labels: map[string]string{"managed-by": "serverpro"}},
		Admin:       Admin{Username: "deploy"},
		Network:     Network{Ingress: "none", Egress: Egress{Mode: "restricted", PhaseLockdownAfterBootstrap: true}},
		Access:      Access{Tailscale: Tailscale{Enabled: true, SSH: true, Tailnet: TokenDefaultTailnet, Tags: []string{"tag:serverpro-server"}, RootPolicy: "check-or-disabled"}},
		Cloudflare:  Cloudflare{},
		Hardening:   Hardening{Profile: "strict", UnattendedUpgrades: true, AppArmor: true, UFW: true, JournaldPersistent: true},
	}
}
