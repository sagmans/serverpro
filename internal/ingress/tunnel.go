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

// TunnelRemoteConfig is what a remotely managed tunnel can reach on the host
// by Cloudflare's own records: its ingress rules, with the
// configuration-wide origin defaults already applied, and whether a private
// network (IP range) or private hostname route sends WARP client traffic to
// the tunnel. Such a route carries every port, sshd's included, and
// cloudflared serves it whatever the configuration's warp-routing setting
// says, so the routes, not that setting, decide.
type TunnelRemoteConfig struct {
	Rules                []TunnelIngressRule
	PrivateNetworkRoute  bool
	PrivateHostnameRoute bool
}
