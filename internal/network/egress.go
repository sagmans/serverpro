package network

import "github.com/assagman/serverpro/internal/config"

func LockdownScript(cfg config.Config) string {
	if cfg.Network.Egress.Mode == "open" {
		return "ufw default allow outgoing\nufw --force reload\n"
	}
	return `set -eu
ufw default deny outgoing
ufw allow out 53
ufw allow out 123/udp
ufw allow out 80/tcp
ufw allow out 443/tcp
ufw allow out 7844/tcp
ufw allow out 7844/udp
ufw allow out 41641/udp
ufw --force reload
`
}
