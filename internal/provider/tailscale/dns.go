package tailscale

import (
	"context"
	"net/http"
	"net/url"

	"github.com/sagmans/serverpro/internal/mesh"
)

// DNSConfig reports tailnet DNS posture: whether MagicDNS is enabled and which
// global nameservers serve as quad100 upstreams. An empty nameserver list with
// MagicDNS enabled is the misconfiguration behind the 2026-07 quad100 SERVFAIL
// incident, so doctor inspects it directly.
func (c Client) DNSConfig(ctx context.Context) (mesh.DNSConfig, error) {
	var preferences struct {
		MagicDNS bool `json:"magicDNS"`
	}
	if err := c.api.Do(ctx, http.MethodGet, "/tailnet/"+url.PathEscape(c.tailnet)+"/dns/preferences", nil, &preferences); err != nil {
		return mesh.DNSConfig{}, err
	}
	var nameservers struct {
		DNS []string `json:"dns"`
	}
	if err := c.api.Do(ctx, http.MethodGet, "/tailnet/"+url.PathEscape(c.tailnet)+"/dns/nameservers", nil, &nameservers); err != nil {
		return mesh.DNSConfig{}, err
	}
	return mesh.DNSConfig{MagicDNS: preferences.MagicDNS, GlobalNameservers: nameservers.DNS}, nil
}
