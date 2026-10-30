package config

import "github.com/sagmans/serverpro/internal/servername"

const (
	defaultCredentialsJSONPath = "~/.config/serverpro/credentials.json"
	// TokenDefaultTailnet is token-relative, so destructive global operations cannot treat it as durable identity.
	TokenDefaultTailnet = "-"
	// DefaultRebootWindow is a low-traffic hour for most operators; the
	// window is in UTC so it does not move with daylight saving.
	DefaultRebootWindow = "02:00"
	// DefaultRebootVerifyTimeout covers a cold start that pulls container
	// images after boot, while still reporting a stuck workload the same night.
	DefaultRebootVerifyTimeout = "10m"
)

func DefaultServer() string {
	return servername.Default
}

func Default() Config {
	return Config{
		Server:      servername.Default,
		Credentials: Credentials{JSONPath: defaultCredentialsJSONPath},
		Compute:     Compute{Location: "fsn1", Size: "cx23", Image: "ubuntu-24.04", Labels: map[string]string{"managed-by": "serverpro"}},
		// StoreConsolePassword defaults on so re-runs stay non-interactive;
		// an explicit false in server YAML still opts out.
		Admin:      Admin{Username: "deploy", StoreConsolePassword: true},
		Network:    Network{Ingress: "none", Egress: Egress{Mode: EgressModeRestricted, PhaseLockdownAfterBootstrap: true}},
		Access:     Access{OpenSSH: OpenSSHDisabled, Tailscale: Tailscale{Enabled: true, SSH: true, Tailnet: TokenDefaultTailnet, Tags: []string{"tag:serverpro-server"}, RootPolicy: "check-or-disabled"}},
		Cloudflare: Cloudflare{},
		Hardening:  Hardening{Profile: "strict", UnattendedUpgrades: true, AppArmor: true, UFW: true, JournaldPersistent: true},
		// The gated reboot defaults on so kernel fixes take effect; an
		// explicit false in server YAML still opts out.
		Reboot: Reboot{Enabled: true, Window: DefaultRebootWindow, VerifyTimeout: DefaultRebootVerifyTimeout},
	}
}
