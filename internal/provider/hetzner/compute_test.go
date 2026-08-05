package hetzner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestComputeProviderMapsCatalogToGenericTerms(t *testing.T) {
	srv := fakeCatalogServer(t, http.StatusOK)
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client {
		return NewWithHTTP(token, srv.URL, srv.Client())
	})
	catalog, diagnostics := provider.Catalog(context.Background(), compute.CatalogQuery{Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"}, Location: "fsn1"})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if len(catalog.Locations) != 1 || catalog.Locations[0].Name != "fsn1" || catalog.Locations[0].City != "Falkenstein" {
		t.Fatalf("bad locations: %+v", catalog.Locations)
	}
	if len(catalog.Sizes) != 1 || catalog.Sizes[0].Name != "cpx22" || catalog.Sizes[0].MemoryGB != 4 {
		t.Fatalf("bad sizes: %+v", catalog.Sizes)
	}
	if len(catalog.Images) != 1 || catalog.Images[0].Name != "ubuntu-24.04" || catalog.Images[0].Architecture != "x86" {
		t.Fatalf("bad images: %+v", catalog.Images)
	}
}

func TestMapCatalogFiltersUnavailableOptions(t *testing.T) {
	catalog := mapCatalog(Catalog{
		Locations: []Location{{Name: "fsn1"}},
		ServerTypes: []ServerType{
			{Name: "deprecated", Deprecated: true},
			{Name: "wrong-location", Locations: []ServerTypeLocation{{Name: "hel1"}}},
			{Name: "active", Locations: []ServerTypeLocation{{Name: "fsn1"}, {Name: "hel1", Deprecated: true}}},
		},
		Images: []Image{
			{Name: "unavailable", Status: "creating"},
			{Name: "deprecated", Status: "available", Deprecated: map[string]any{"at": "2026-01-01"}},
			{Name: "active", Status: "available"},
		},
	}, "fsn1")

	if len(catalog.Sizes) != 1 || catalog.Sizes[0].Name != "active" || len(catalog.Sizes[0].Locations) != 1 || catalog.Sizes[0].Locations[0] != "fsn1" {
		t.Fatalf("sizes = %+v", catalog.Sizes)
	}
	if len(catalog.Images) != 1 || catalog.Images[0].Name != "active" {
		t.Fatalf("images = %+v", catalog.Images)
	}
}

func TestComputeProviderDoctorReportsGenericDiagnostics(t *testing.T) {
	srv := fakeCatalogServer(t, http.StatusUnauthorized)
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client {
		return NewWithHTTP(token, srv.URL, srv.Client())
	})
	secret := strings.Repeat("x", 12)
	diagnostics := provider.Doctor(context.Background(), compute.Account{Name: "prod", Provider: "hetzner", Token: secret})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatalf("expected failure diagnostics")
	}
	var statusErr *httpjson.StatusError
	if strings.Contains(diagnostics.Err().Error(), secret) || !strings.Contains(diagnostics.Err().Error(), "provider credential validation failed") || !errors.As(diagnostics.Err(), &statusErr) {
		t.Fatalf("bad diagnostic error: %v", diagnostics.Err())
	}
}

func TestComputeProviderCreateUsesGenericIntentAndDenyPublicPolicy(t *testing.T) {
	var firewallPayload map[string]any
	var serverPayload map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			if err := json.NewDecoder(r.Body).Decode(&firewallPayload); err != nil {
				handlerErr.Record(w, "decode firewall payload: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/servers":
			if err := json.NewDecoder(r.Body).Decode(&serverPayload); err != nil {
				handlerErr.Record(w, "decode server payload: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","public_net":{"ipv4":{"ip":"203.0.113.10"}}},"action":{"id":7,"status":"running"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/actions/7":
			_, _ = w.Write([]byte(`{"action":{"id":7,"status":"success"}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Intent:        compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04", Labels: map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}},
		BootstrapData: "#cloud-config",
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if record.ID != "42" || !ok || policyID != "9" || len(record.ProviderState) != 0 || record.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("bad record: %+v", record)
	}
	if len(firewallPayload["rules"].([]any)) != 0 || !payloadLabelsMatch(firewallPayload, "prod", "web") {
		t.Fatalf("bad access policy payload: %+v", firewallPayload)
	}
	if serverPayload["server_type"] != "cpx22" || serverPayload["image"] != "ubuntu-24.04" || serverPayload["location"] != "fsn1" || serverPayload["user_data"] != "#cloud-config" || !payloadLabelsMatch(serverPayload, "prod", "web") {
		t.Fatalf("bad server payload: %+v", serverPayload)
	}
	firewall := serverPayload["firewalls"].([]any)[0].(map[string]any)
	if firewall["firewall"].(float64) != 9 {
		t.Fatalf("server missing access policy attachment: %+v", serverPayload)
	}
}

func TestComputeProviderCreateReturnsAccessPolicyCheckpointOnServerFailure(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/servers":
			http.Error(w, "server create failed", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/servers" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"servers":[]}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatal("expected server create failure")
	}
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if record.ID != "" || !ok || policyID != "9" || len(record.ProviderState) != 0 {
		t.Fatalf("access policy checkpoint missing: %+v", record)
	}
}

func TestComputeProviderCreateRejectsUnsafeCheckpointAccessPolicy(t *testing.T) {
	tests := []struct {
		name         string
		firewallJSON string
		wantError    string
	}{
		{
			name:         "foreign ownership",
			firewallJSON: `{"firewall":{"id":9,"name":"prod-web-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"api"},"rules":[],"applied_to":[]}}`,
			wantError:    "ownership mismatch",
		},
		{
			name:         "broadened rules",
			firewallJSON: `{"firewall":{"id":9,"name":"prod-web-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"},"rules":[{}],"applied_to":[]}}`,
			wantError:    "unexpected rules",
		},
		{
			name:         "direct attachment",
			firewallJSON: `{"firewall":{"id":9,"name":"prod-web-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"},"rules":[],"applied_to":[{}]}}`,
			wantError:    "attachment",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverPosts := 0
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			defer handlerErr.Check()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/firewalls/9":
					_, _ = w.Write([]byte(test.firewallJSON))
				case r.Method == http.MethodPost && r.URL.Path == "/servers":
					serverPosts++
					_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web"},"action":{"id":7,"status":"running"}}`))
				case r.Method == http.MethodGet && r.URL.Path == "/actions/7":
					_, _ = w.Write([]byte(`{"action":{"id":7,"status":"success"}}`))
				default:
					handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
			_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
				Account:          compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
				Intent:           compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"},
				ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "9"}},
			})
			if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), test.wantError) || serverPosts != 0 {
				t.Fatalf("diagnostics=%v serverPosts=%d", diagnostics.Err(), serverPosts)
			}
		})
	}
}

func TestComputeProviderCreateAcceptsSafeCheckpointAccessPolicy(t *testing.T) {
	firewallGets := 0
	serverPosts := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/9":
			firewallGets++
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"},"rules":[],"applied_to":[]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/servers":
			serverPosts++
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web"},"action":{"id":7,"status":"running"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/actions/7":
			_, _ = w.Write([]byte(`{"action":{"id":7,"status":"success"}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:          compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Intent:           compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"},
		ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "9"}},
	})
	if !diagnostics.Passed() || firewallGets != 1 || serverPosts != 1 {
		t.Fatalf("diagnostics=%v firewallGets=%d serverPosts=%d", diagnostics.Err(), firewallGets, serverPosts)
	}
}

func TestComputeProviderDeleteIgnoresMissingAccessPolicy(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"action":{"id":7,"status":"running"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/actions/7":
			_, _ = w.Write([]byte(`{"action":{"id":7,"status":"success"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/9":
			http.NotFound(w, r)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"access_policy_id": "9"}},
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
}

func TestComputeProviderDeleteContinuesAfterServerAlreadyDeleted(t *testing.T) {
	deletedFirewall := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/9":
			_, _ = w.Write([]byte(`{"firewall":{"id":9,"name":"prod-web-deny-public","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/9":
			deletedFirewall = true
			w.WriteHeader(http.StatusNoContent)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web", ProviderState: map[string]string{"access_policy_id": "9"}},
	})
	if !diagnostics.Passed() || !deletedFirewall {
		t.Fatalf("diagnostics=%v deletedFirewall=%t", diagnostics.Err(), deletedFirewall)
	}
}

func TestComputeProviderPowerRejectsOwnershipMismatch(t *testing.T) {
	actionCalled := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
			_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","labels":{"serverpro.namespace":"prod","serverpro.server":"api"}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/servers/42/actions/poweron":
			actionCalled = true
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
		Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
		Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web"},
		Action:  compute.PowerStart,
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership mismatch") || actionCalled {
		t.Fatalf("diagnostics=%v actionCalled=%t", diagnostics.Err(), actionCalled)
	}
}

func TestComputeProviderPowerUsesNativeActions(t *testing.T) {
	for _, test := range []struct {
		name   string
		action compute.PowerAction
		path   string
	}{
		{name: "start", action: compute.PowerStart, path: "/servers/42/actions/poweron"},
		{name: "stop", action: compute.PowerStop, path: "/servers/42/actions/shutdown"},
		{name: "restart", action: compute.PowerRestart, path: "/servers/42/actions/reboot"},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := 0
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			defer handlerErr.Check()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == test.path:
					actions++
					_, _ = w.Write([]byte(`{"action":{"id":8,"status":"running"}}`))
				case r.Method == http.MethodGet && r.URL.Path == "/actions/8":
					_, _ = w.Write([]byte(`{"action":{"id":8,"status":"success"}}`))
				case r.Method == http.MethodGet && r.URL.Path == "/servers/42":
					_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","status":"running","labels":{"managed-by":"serverpro","serverpro.namespace":"prod","serverpro.server":"web"},"public_net":{"ipv4":{"ip":"203.0.113.10"},"ipv6":{"ip":""}}}}`))
				default:
					handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
			status, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
				Account: compute.Account{Name: "prod", Provider: "hetzner", Token: "token"},
				Record:  compute.ServerRecord{Provider: "hetzner", Account: "prod", ID: "42", Name: "prod-web", Namespace: "prod", Server: "web"},
				Action:  test.action,
			})
			if !diagnostics.Passed() || actions != 1 || status.Power != "running" {
				t.Fatalf("diagnostics=%v actions=%d status=%+v", diagnostics.Err(), actions, status)
			}
		})
	}
}

func payloadLabelsMatch(payload map[string]any, namespace, server string) bool {
	labels, ok := payload["labels"].(map[string]any)
	return ok && labels["managed-by"] == "serverpro" && labels["serverpro-namespace"] == namespace && labels["serverpro-server"] == server
}

func fakeCatalogServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	t.Cleanup(handlerErr.Check)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" && status == http.StatusOK {
			handlerErr.Record(w, "missing auth header: %s", r.Header.Get("Authorization"))
			return
		}
		if status != http.StatusOK {
			http.Error(w, "bad "+r.Header.Get("Authorization"), status)
			return
		}
		switch r.URL.Path {
		case "/locations":
			_, _ = w.Write([]byte(`{"locations":[{"id":1,"name":"fsn1","description":"Falkenstein","country":"DE","city":"Falkenstein","network_zone":"eu-central"}],"meta":{"pagination":{"next_page":null}}}`))
		case "/server_types":
			_, _ = w.Write([]byte(`{"server_types":[{"id":1,"name":"cpx22","description":"CPX 22","cores":2,"memory":4,"disk":80,"architecture":"x86","locations":[{"name":"fsn1"}]}],"meta":{"pagination":{"next_page":null}}}`))
		case "/images":
			_, _ = w.Write([]byte(`{"images":[{"id":1,"name":"ubuntu-24.04","description":"Ubuntu 24.04","architecture":"x86","status":"available","os_flavor":"ubuntu","os_version":"24.04"}],"meta":{"pagination":{"next_page":null}}}`))
		default:
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
		}
	}))
}
