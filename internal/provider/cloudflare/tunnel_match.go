package cloudflare

import "fmt"

// MatchTunnelByName requires one exact account-wide name match so provision
// and import reject the same ambiguous ownership evidence.
func MatchTunnelByName(tunnels []Tunnel, name string) (Tunnel, bool, error) {
	var match Tunnel
	found := false
	for _, tunnel := range tunnels {
		if tunnel.Name != name {
			continue
		}
		if found {
			return Tunnel{}, false, fmt.Errorf("cloudflare tunnel %q is ambiguous", name)
		}
		match = tunnel
		found = true
	}
	return match, found, nil
}
