package vultr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
)

func TestComputeProviderCreateRemovesOnlyExactLegacySTUNRulesFromCheckpoint(t *testing.T) {
	deleted := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9/rules":
			_, _ = w.Write([]byte(`{"firewall_rules":[{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0,"notes":"tailscale wireguard"},{"id":2,"action":"accept","ip_type":"v6","protocol":"udp","port":"41641","subnet":"::","subnet_size":0,"notes":"tailscale wireguard"},{"id":3,"action":"accept","ip_type":"v4","protocol":"udp","port":"3478","subnet":"0.0.0.0","subnet_size":0,"notes":"tailscale stun"},{"id":4,"action":"accept","ip_type":"v4","protocol":"udp","port":"3478","subnet":"0.0.0.0","subnet_size":0,"notes":"manual"}],"meta":{"links":{"next":"","prev":""}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9/rules/3":
			deleted["3"] = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(string) Client { return NewWithHTTP("token", srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:        compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "2284"},
		ProviderState: map[string]string{"firewall_group_id": "fw-9"},
	})
	if !diagnostics.Passed() || !deleted["3"] || deleted["4"] {
		t.Fatalf("diagnostics=%v deleted=%v", diagnostics.Err(), deleted)
	}
}
