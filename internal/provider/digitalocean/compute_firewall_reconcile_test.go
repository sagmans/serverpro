package digitalocean

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
)

func TestComputeProviderCreateRemovesOnlyExactLegacySTUNRuleFromCheckpoint(t *testing.T) {
	var removed map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"],"inbound_rules":[{"protocol":"udp","ports":"3478","sources":{"addresses":["0.0.0.0/0","::/0"]}},{"protocol":"udp","ports":"3478","sources":{"tags":["manual"]}},{"protocol":"udp","ports":"41641","sources":{"addresses":["0.0.0.0/0","::/0"]}}]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9/rules":
			if err := json.NewDecoder(r.Body).Decode(&removed); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","networks":{"v4":[],"v6":[]}}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(string) Client { return NewWithHTTP("token", srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:        digitalOceanIntent(),
		ProviderState: map[string]string{"firewall_id": "fw-9"},
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	rules, ok := removed["inbound_rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("removed rules = %#v", removed)
	}
	rule := rules[0].(map[string]any)
	if rule["protocol"] != "udp" || rule["ports"] != "3478" {
		t.Fatalf("removed non-legacy rule: %#v", rule)
	}
}

func TestComputeProviderCreateRejectsMismatchedCheckpointedFirewallBeforeRuleDelete(t *testing.T) {
	mutated := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9" {
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"other-deny-public","tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"]}}`))
			return
		}
		mutated = true
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(string) Client { return NewWithHTTP("token", srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:        digitalOceanIntent(),
		ProviderState: map[string]string{"firewall_id": "fw-9"},
	})
	if diagnostics.Passed() || mutated {
		t.Fatalf("diagnostics=%v mutated=%t", diagnostics.Err(), mutated)
	}
}
