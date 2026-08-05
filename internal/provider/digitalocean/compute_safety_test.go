package digitalocean

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
)

func TestComputeProviderDeleteIgnoresMissingFirewall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
}

func TestComputeProviderDeleteContinuesAfterDropletAlreadyDeleted(t *testing.T) {
	deletedFirewall := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
			deletedFirewall = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if !diagnostics.Passed() || !deletedFirewall {
		t.Fatalf("diagnostics=%v deletedFirewall=%t", diagnostics.Err(), deletedFirewall)
	}
}

func TestComputeProviderDeleteRejectsOwnershipMismatch(t *testing.T) {
	deleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"other-web","tags":["managed-by:serverpro","serverpro.namespace:other","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			deleteCalled = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership mismatch") || deleteCalled {
		t.Fatalf("diagnostics=%v deleteCalled=%t", diagnostics.Err(), deleteCalled)
	}
}

func TestComputeProviderDeleteRejectsMismatchedFirewall(t *testing.T) {
	firewallDeleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"other-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
			firewallDeleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership") || firewallDeleteCalled {
		t.Fatalf("diagnostics=%v firewallDeleteCalled=%t", diagnostics.Err(), firewallDeleteCalled)
	}
}

func TestComputeProviderCreateRedactsBootstrapDataFromDropletErrors(t *testing.T) {
	authKey := strings.Join([]string{"tskey", "auth", "sensitive"}, "-")
	passwordHash := "$6$rounds=100000$abcdefghijklmnop$" + strings.Repeat("A", 86)
	bootstrapData := "#cloud-config\n" + authKey + "\nhashed_passwd: " + passwordHash + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case handleCreateTag(t, w, r):
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			http.Error(w, "echoed user_data "+bootstrapData, http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/droplets" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"droplets":[],"links":{"pages":{}},"meta":{"total":0}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:        digitalOceanIntent(),
		BootstrapData: bootstrapData,
	})
	message := diagnostics.Err().Error()
	if diagnostics.Passed() || strings.Contains(message, authKey) || strings.Contains(message, passwordHash) || strings.Contains(message, bootstrapData) {
		t.Fatalf("bootstrap secrets leaked in diagnostics: %v", diagnostics.Err())
	}
}

func digitalOceanRecord() compute.ServerRecord {
	return compute.ServerRecord{
		Provider:  "digitalocean",
		Account:   "prod",
		ID:        "3164444",
		Name:      "prod-web",
		Namespace: "prod",
		Server:    "web",
		ProviderState: map[string]string{
			"firewall_id": "fw-9",
		},
	}
}
