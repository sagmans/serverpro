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
