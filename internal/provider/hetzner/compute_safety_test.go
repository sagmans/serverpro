package hetzner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
)

func TestComputeProviderDeleteCleansAccessPolicyWithoutServerID(t *testing.T) {
	deletedFirewall := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/9":
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/9":
			deletedFirewall = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"access_policy_id": "9"}},
	})
	if !diagnostics.Passed() || !deletedFirewall {
		t.Fatalf("diagnostics=%v deletedFirewall=%t", diagnostics.Err(), deletedFirewall)
	}
}

func TestComputeProviderDeleteRejectsMismatchedAccessPolicy(t *testing.T) {
	deleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/9":
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"other-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"other","serverpro.server":"web"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/9":
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"access_policy_id": "9"}},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership") || deleteCalled {
		t.Fatalf("diagnostics=%v deleteCalled=%t", diagnostics.Err(), deleteCalled)
	}
}

func TestComputeProviderDeleteRejectsMissingLiveNamespaceLabel(t *testing.T) {
	deleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","labels":{"managed-by":"serverpro","serverpro.server":"web"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/servers/42":
			deleteCalled = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "serverpro.namespace") || deleteCalled {
		t.Fatalf("diagnostics=%v deleteCalled=%t", diagnostics.Err(), deleteCalled)
	}
}

func TestComputeProviderDeleteRejectsUnlabeledLiveServer(t *testing.T) {
	deleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/servers/42":
			deleteCalled = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership") || deleteCalled {
		t.Fatalf("diagnostics=%v deleteCalled=%t", diagnostics.Err(), deleteCalled)
	}
}

func TestComputeProviderPowerRejectsMissingLiveNamespaceLabel(t *testing.T) {
	actionCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","labels":{"managed-by":"serverpro","serverpro.server":"web"}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/servers/42/actions/poweron":
			actionCalled = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web"},
		Action:  compute.PowerStart,
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "serverpro.namespace") || actionCalled {
		t.Fatalf("diagnostics=%v actionCalled=%t", diagnostics.Err(), actionCalled)
	}
}

func TestComputeProviderDeleteReportsAccessPolicyDeleteFailureAfterServerDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"action":{"id":7,"status":"running"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/actions/7":
			_, _ = w.Write([]byte(`{"action":{"id":7,"status":"success"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/9":
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/9":
			http.Error(w, "firewall delete failed", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"access_policy_id": "9"}},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "access policy") {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
}

func TestComputeProviderCreateReconcilesServerByNameOnCreateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/servers":
			http.Error(w, "server create response lost", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/servers" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"servers":[{"id":42,"name":"prod-web","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"},"public_net":{"ipv4":{"ip":"203.0.113.10"},"ipv6":{"ip":""}}}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04", Labels: map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}},
	})
	if diagnostics.Passed() || record.ID != "42" || record.ProviderState["access_policy_id"] != "9" {
		t.Fatalf("record=%+v diagnostics=%v", record, diagnostics.Err())
	}
}

func TestComputeProviderCreateRedactsBootstrapDataFromServerErrors(t *testing.T) {
	const authKey = "tskey-auth-sensitive"
	const passwordHash = "$6$rounds=100000$abcdefghijklmnop$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	bootstrapData := "#cloud-config\n" + authKey + "\nhashed_passwd: " + passwordHash + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/servers":
			http.Error(w, "echoed user_data "+bootstrapData, http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/servers" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"servers":[]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Intent:        compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"},
		BootstrapData: bootstrapData,
	})
	message := diagnostics.Err().Error()
	if diagnostics.Passed() || strings.Contains(message, authKey) || strings.Contains(message, passwordHash) || strings.Contains(message, bootstrapData) {
		t.Fatalf("bootstrap secrets leaked in diagnostics: %v", diagnostics.Err())
	}
}
