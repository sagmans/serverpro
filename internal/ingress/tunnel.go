package ingress

import "fmt"

// Tunnel is provider-neutral managed ingress connector identity.
type Tunnel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// MatchTunnelByName applies exact zero/one/ambiguous semantics.
func MatchTunnelByName(tunnels []Tunnel, name string) (Tunnel, bool, error) {
	var match Tunnel
	found := false
	for _, tunnel := range tunnels {
		if tunnel.Name != name {
			continue
		}
		if found {
			return Tunnel{}, false, fmt.Errorf("tunnel %q is ambiguous", name)
		}
		match, found = tunnel, true
	}
	return match, found, nil
}

// TunnelIngressRule is one rule of a remotely managed tunnel's ingress. It
// keeps only what decides where the rule can reach: the origin service, and
// the origin settings that let a client pick any destination (bastion mode
// and a SOCKS proxy). Hostnames stay with the provider, because no caller
// needs them and they would only leak into errors and evidence.
type TunnelIngressRule struct {
	Service     string
	BastionMode bool
	ProxyType   string
}

// TunnelRemoteConfig is what a remotely managed tunnel's configuration can
// reach on the host: its ingress rules, with the configuration-wide origin
// defaults already applied, and whether WARP routing carries private network
// traffic, sshd's port included, through the tunnel.
type TunnelRemoteConfig struct {
	Rules       []TunnelIngressRule
	WARPRouting bool
}
