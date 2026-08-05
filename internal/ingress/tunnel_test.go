package ingress

import (
	"strings"
	"testing"
)

func TestMatchTunnelByNameHandlesZeroOneAndAmbiguous(t *testing.T) {
	tunnels := []Tunnel{{ID: "1", Name: "app"}, {ID: "2", Name: "other"}}
	if got, found, err := MatchTunnelByName(tunnels, "app"); err != nil || !found || got.ID != "1" {
		t.Fatalf("got=%+v found=%t err=%v", got, found, err)
	}
	if _, found, err := MatchTunnelByName(tunnels, "missing"); err != nil || found {
		t.Fatalf("found=%t err=%v", found, err)
	}
	if _, _, err := MatchTunnelByName(append(tunnels, Tunnel{ID: "3", Name: "app"}), "app"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err=%v", err)
	}
}
