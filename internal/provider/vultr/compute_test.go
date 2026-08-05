package vultr

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

func TestComputeProviderNameAndCapabilities(t *testing.T) {
	p := NewComputeProvider(nil)
	if p.Name() != "vultr" {
		t.Fatalf("name = %q", p.Name())
	}
	caps := p.Capabilities(context.Background())
	if !caps.CreateServer || !caps.DeleteServer || !caps.PowerServer || !caps.Catalog {
		t.Fatalf("capabilities = %+v", caps)
	}
}

func TestComputeProviderMapsCatalogToGenericTerms(t *testing.T) {
	srv := fakeCatalogServer(t, http.StatusOK)
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client {
		return NewWithHTTP(token, srv.URL, srv.Client())
	})
	catalog, diagnostics := provider.Catalog(context.Background(), compute.CatalogQuery{
		Account:  compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Location: "ewr",
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if len(catalog.Locations) != 1 || catalog.Locations[0].Name != "ewr" || catalog.Locations[0].City != "New York" {
		t.Fatalf("bad locations: %+v", catalog.Locations)
	}
	if len(catalog.Sizes) != 1 || catalog.Sizes[0].Name != "vc2-1c-2gb" || catalog.Sizes[0].MemoryGB != 2 {
		t.Fatalf("bad sizes: %+v", catalog.Sizes)
	}
	if len(catalog.Images) != 1 || catalog.Images[0].Name != "1743" || catalog.Images[0].OSFlavor != "ubuntu" {
		t.Fatalf("bad images: %+v", catalog.Images)
	}
}

func TestComputeProviderDoctorReportsGenericDiagnostics(t *testing.T) {
	srv := fakeCatalogServer(t, http.StatusUnauthorized)
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client {
		return NewWithHTTP(token, srv.URL, srv.Client())
	})
	secret := strings.Repeat("x", 12)
	diagnostics := provider.Doctor(context.Background(), compute.Account{Name: "prod", Provider: "vultr", Token: secret})
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
	var firewallRulePayloads []map[string]any
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
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9/rules":
			_, _ = w.Write([]byte(`{"firewall_rules":[],"meta":{"links":{"next":"","prev":""}}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				handlerErr.Record(w, "decode firewall rule payload: %v", err)
				return
			}
			firewallRulePayloads = append(firewallRulePayloads, payload)
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			if err := json.NewDecoder(r.Body).Decode(&serverPayload); err != nil {
				handlerErr.Record(w, "decode server payload: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"active","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:        compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "1743", Labels: map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}},
		BootstrapData: "#cloud-config",
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if record.ID != "abc-123" || !ok || policyID != "fw-9" || len(record.ProviderState) != 0 || record.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("bad record: %+v", record)
	}
	if firewallPayload["description"] != "prod-web-deny-public" {
		t.Fatalf("bad firewall payload: %+v", firewallPayload)
	}
	if len(firewallRulePayloads) != 4 {
		t.Fatalf("expected 4 firewall rules, got %d: %+v", len(firewallRulePayloads), firewallRulePayloads)
	}
	gotPorts := make(map[string]bool)
	for _, p := range firewallRulePayloads {
		gotPorts[p["port"].(string)+"/"+p["ip_type"].(string)] = true
	}
	for _, want := range []string{"41641/v4", "41641/v6", "3478/v4", "3478/v6"} {
		if !gotPorts[want] {
			t.Fatalf("missing firewall rule for %s", want)
		}
	}
	if serverPayload["region"] != "ewr" || serverPayload["plan"] != "vc2-1c-2gb" || serverPayload["os_id"] != float64(1743) {
		t.Fatalf("bad server payload: %+v", serverPayload)
	}
	if serverPayload["firewall_group_id"] != "fw-9" || serverPayload["enable_ipv6"] != false {
		t.Fatalf("missing ipv4-only firewall attachment: %+v", serverPayload)
	}
	if !payloadHasTags(serverPayload, "managed-by:serverpro", "serverpro-namespace:prod", "serverpro-server:web") {
		t.Fatalf("server ownership tags missing: %+v", serverPayload)
	}
}

func TestComputeProviderCreateReturnsFirewallCheckpointOnServerFailure(t *testing.T) {
	ruleRequestCount := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9/rules":
			_, _ = w.Write([]byte(`{"firewall_rules":[],"meta":{"links":{"next":"","prev":""}}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			ruleRequestCount++
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			http.Error(w, "server create failed", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/instances" && r.URL.Query().Get("label") == "prod-web":
			_, _ = w.Write([]byte(`{"instances":[],"meta":{"links":{"next":"","prev":""}}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "1743"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatal("expected server create failure")
	}
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if record.ID != "" || !ok || policyID != "fw-9" || len(record.ProviderState) != 0 {
		t.Fatalf("firewall checkpoint missing: %+v", record)
	}
	if ruleRequestCount != 4 {
		t.Fatalf("expected 4 firewall rule requests, got %d", ruleRequestCount)
	}
}

func TestComputeProviderCreateReturnsFirewallCheckpointOnRuleFailure(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9/rules":
			_, _ = w.Write([]byte(`{"firewall_rules":[],"meta":{"links":{"next":"","prev":""}}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			http.Error(w, "rule create failed", http.StatusBadGateway)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "1743"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatal("expected firewall rule failure")
	}
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if record.ID != "" || !ok || policyID != "fw-9" || len(record.ProviderState) != 0 {
		t.Fatalf("firewall checkpoint missing: %+v", record)
	}
}

func TestComputeProviderCreateReconcilesMissingCheckpointFirewallRules(t *testing.T) {
	rulePosts := 0
	instancePosts := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public","instance_count":0}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9/rules":
			_, _ = w.Write([]byte(`{"firewall_rules":[{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0},{"id":2,"action":"accept","ip_type":"v6","protocol":"udp","port":"41641","subnet":"::","subnet_size":0}],"meta":{"links":{"next":"","prev":""}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls/fw-9/rules":
			rulePosts++
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":3,"action":"accept"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			instancePosts++
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10"}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:          compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:           compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "1743"},
		ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "fw-9"}},
	})
	if !diagnostics.Passed() || rulePosts != 2 || instancePosts != 1 {
		t.Fatalf("diagnostics=%v rulePosts=%d instancePosts=%d", diagnostics.Err(), rulePosts, instancePosts)
	}
}

func TestComputeProviderCreateDoesNotDuplicateCompleteCheckpointFirewallRules(t *testing.T) {
	rulePosts := 0
	ruleLists := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public","instance_count":0}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9/rules":
			ruleLists++
			_, _ = w.Write([]byte(`{"firewall_rules":[{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0},{"id":2,"action":"accept","ip_type":"v6","protocol":"udp","port":"41641","subnet":"::","subnet_size":0},{"id":3,"action":"accept","ip_type":"v4","protocol":"udp","port":"3478","subnet":"0.0.0.0","subnet_size":0},{"id":4,"action":"accept","ip_type":"v6","protocol":"udp","port":"3478","subnet":"::","subnet_size":0}],"meta":{"links":{"next":"","prev":""}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls/fw-9/rules":
			rulePosts++
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":5,"action":"accept"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10"}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:        compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "1743"},
		ProviderState: map[string]string{"firewall_group_id": "fw-9"},
	})
	if !diagnostics.Passed() || ruleLists != 1 || rulePosts != 0 {
		t.Fatalf("diagnostics=%v ruleLists=%d rulePosts=%d", diagnostics.Err(), ruleLists, rulePosts)
	}
}

func TestComputeProviderCreateRejectsUnsafeCheckpointFirewallGroup(t *testing.T) {
	const requiredRules = `{"firewall_rules":[{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0},{"id":2,"action":"accept","ip_type":"v6","protocol":"udp","port":"41641","subnet":"::","subnet_size":0},{"id":3,"action":"accept","ip_type":"v4","protocol":"udp","port":"3478","subnet":"0.0.0.0","subnet_size":0},{"id":4,"action":"accept","ip_type":"v6","protocol":"udp","port":"3478","subnet":"::","subnet_size":0}],"meta":{"links":{"next":"","prev":""}}}`
	tests := []struct {
		name      string
		groupJSON string
		rulesJSON string
		wantError string
	}{
		{
			name:      "foreign identity",
			groupJSON: `{"firewall_group":{"id":"fw-9","description":"staging-web-deny-public","instance_count":0}}`,
			rulesJSON: requiredRules,
			wantError: "ownership mismatch",
		},
		{
			name:      "direct attachment",
			groupJSON: `{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public","instance_count":1}}`,
			rulesJSON: requiredRules,
			wantError: "attachment",
		},
		{
			name:      "broadened rule",
			groupJSON: `{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public","instance_count":0}}`,
			rulesJSON: `{"firewall_rules":[{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0},{"id":2,"action":"accept","ip_type":"v6","protocol":"udp","port":"41641","subnet":"::","subnet_size":0},{"id":3,"action":"accept","ip_type":"v4","protocol":"udp","port":"3478","subnet":"0.0.0.0","subnet_size":0},{"id":4,"action":"accept","ip_type":"v6","protocol":"udp","port":"3478","subnet":"::","subnet_size":0},{"id":5,"action":"accept","ip_type":"v4","protocol":"tcp","port":"22","subnet":"0.0.0.0","subnet_size":0}],"meta":{"links":{"next":"","prev":""}}}`,
			wantError: "unexpected rule",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instancePosts := 0
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			defer handlerErr.Check()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
					_, _ = w.Write([]byte(test.groupJSON))
				case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9/rules":
					_, _ = w.Write([]byte(test.rulesJSON))
				case r.Method == http.MethodPost && r.URL.Path == "/instances":
					instancePosts++
					_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10"}}`))
				default:
					handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
				}
			}))
			defer srv.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
			_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
				Account:          compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
				Intent:           compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "1743"},
				ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "fw-9"}},
			})
			if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), test.wantError) || instancePosts != 0 {
				t.Fatalf("diagnostics=%v instancePosts=%d", diagnostics.Err(), instancePosts)
			}
		})
	}
}

func TestComputeProviderStatusReturnsLiveState(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/instances/abc-123" {
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"active","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	status, diagnostics := provider.Status(context.Background(), compute.ServerRef{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Record:  compute.ServerRecord{Provider: "vultr", ID: "abc-123", Name: "prod-web", Namespace: "prod", Server: "web"},
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if status.Power != "running" || status.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("bad status: %+v", status)
	}
}

func payloadHasTags(payload map[string]any, wants ...string) bool {
	tags, ok := payload["tags"].([]any)
	if !ok {
		return false
	}
	got := map[string]bool{}
	for _, raw := range tags {
		got[raw.(string)] = true
	}
	for _, want := range wants {
		if !got[want] {
			return false
		}
	}
	return true
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
		case "/regions":
			_, _ = w.Write([]byte(`{"regions":[{"id":"ewr","city":"New York","country":"US","continent":"North America"}],"meta":{"links":{"next":"","prev":""}}}`))
		case "/plans":
			_, _ = w.Write([]byte(`{"plans":[{"id":"vc2-1c-2gb","vcpu_count":1,"ram":2048,"disk":55,"disk_count":1,"bandwidth":2048,"monthly_cost":5.0,"type":"vc2","locations":["ewr"]}],"meta":{"links":{"next":"","prev":""}}}`))
		case "/os":
			_, _ = w.Write([]byte(`{"os":[{"id":1743,"name":"Ubuntu 24.04 LTS x64","arch":"x64","family":"ubuntu"}],"meta":{"links":{"next":"","prev":""}}}`))
		default:
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
		}
	}))
}
