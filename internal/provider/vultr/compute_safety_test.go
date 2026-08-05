package vultr

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
)

func TestComputeProviderDeleteIgnoresMissingFirewallGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/instances/abc-123":
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"active","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/instances/abc-123":
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
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Record:  compute.ServerRecord{Provider: "vultr", Account: "prod", ID: "abc-123", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"firewall_group_id": "fw-9"}},
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
}

func TestComputeProviderDeleteContinuesAfterInstanceAlreadyDeleted(t *testing.T) {
	deletedFirewall := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/instances/abc-123":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
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
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Record:  compute.ServerRecord{Provider: "vultr", Account: "prod", ID: "abc-123", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"firewall_group_id": "fw-9"}},
	})
	if !diagnostics.Passed() || !deletedFirewall {
		t.Fatalf("diagnostics=%v deletedFirewall=%t", diagnostics.Err(), deletedFirewall)
	}
}

func TestComputeProviderDeleteRejectsOwnershipMismatch(t *testing.T) {
	deleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/instances/abc-123":
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"other-web","tags":["managed-by:serverpro","serverpro.namespace:other","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/instances/abc-123":
			deleteCalled = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Record:  compute.ServerRecord{Provider: "vultr", Account: "prod", ID: "abc-123", Name: "prod-web", Namespace: "prod", Server: "web"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership mismatch") || deleteCalled {
		t.Fatalf("diagnostics=%v deleteCalled=%t", diagnostics.Err(), deleteCalled)
	}
}

func TestComputeProviderDeleteRejectsMismatchedFirewallGroup(t *testing.T) {
	firewallDeleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/instances/abc-123":
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/instances/abc-123":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"other-deny-public"}}`))
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
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Record:  compute.ServerRecord{Provider: "vultr", Account: "prod", ID: "abc-123", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"firewall_group_id": "fw-9"}},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership") || firewallDeleteCalled {
		t.Fatalf("diagnostics=%v firewallDeleteCalled=%t", diagnostics.Err(), firewallDeleteCalled)
	}
}

func TestComputeProviderPowerRejectsOwnershipMismatch(t *testing.T) {
	actionCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/instances/abc-123":
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:api"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances/abc-123/start":
			actionCalled = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Record:  compute.ServerRecord{Provider: "vultr", Account: "prod", ID: "abc-123", Name: "prod-web", Namespace: "prod", Server: "web"},
		Action:  compute.PowerStart,
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership mismatch") || actionCalled {
		t.Fatalf("diagnostics=%v actionCalled=%t", diagnostics.Err(), actionCalled)
	}
}

func TestComputeProviderRestartUsesNativeRebootAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/instances/abc-123/reboot":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/instances/abc-123":
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"active","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	status, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Record:  compute.ServerRecord{Provider: "vultr", Account: "prod", ID: "abc-123", Name: "prod-web", Namespace: "prod", Server: "web"},
		Action:  compute.PowerRestart,
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if status.Power != "running" {
		t.Fatalf("status=%+v", status)
	}
}

func TestComputeProviderCreateReconcilesInstanceByLabelOnCreateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			http.Error(w, "server create response lost", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/instances" && r.URL.Query().Get("label") == "prod-web":
			_, _ = w.Write([]byte(`{"instances":[{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"active","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}],"meta":{"links":{"next":"","prev":""}}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "2284", Labels: map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}},
	})
	if diagnostics.Passed() || record.ID != "abc-123" || record.ProviderState["firewall_group_id"] != "fw-9" {
		t.Fatalf("record=%+v diagnostics=%v", record, diagnostics.Err())
	}
}

func TestComputeProviderCreateRedactsBootstrapDataFromServerErrors(t *testing.T) {
	const authKey = "tskey-auth-sensitive"
	const passwordHash = "$6$rounds=100000$abcdefghijklmnop$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	bootstrapData := "#cloud-config\n" + authKey + "\nhashed_passwd: " + passwordHash + "\n"
	encodedBootstrapData := base64.StdEncoding.EncodeToString([]byte(bootstrapData))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			http.Error(w, "echoed user_data "+bootstrapData, http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/instances" && r.URL.Query().Get("label") == "prod-web":
			_, _ = w.Write([]byte(`{"instances":[],"meta":{"links":{"next":"","prev":""}}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:        compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "2284"},
		BootstrapData: bootstrapData,
	})
	message := diagnostics.Err().Error()
	if diagnostics.Passed() || strings.Contains(message, authKey) || strings.Contains(message, passwordHash) || strings.Contains(message, bootstrapData) || strings.Contains(message, encodedBootstrapData) {
		t.Fatalf("bootstrap secrets leaked in diagnostics: %v", diagnostics.Err())
	}
}
