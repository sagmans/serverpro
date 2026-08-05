package vultr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
)

func TestComputeProviderSupportsOnlyBootstrapImageReference(t *testing.T) {
	provider := NewComputeProvider(nil)
	if !provider.SupportsImageReference("2284") || provider.SupportsImageReference("1743") {
		t.Fatal("provider image policy must match Ubuntu 24.04 exactly")
	}
}

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
	if len(catalog.Images) != 1 || catalog.Images[0].Name != "2284" || catalog.Images[0].OSFlavor != "ubuntu" {
		t.Fatalf("bad images: %+v", catalog.Images)
	}
}

func TestMapCatalogFiltersImagesIncompatibleWithBootstrap(t *testing.T) {
	catalog := mapCatalog(Catalog{OS: []OS{
		{ID: 2284, Name: "Ubuntu 24.04 LTS x64", Family: "ubuntu"},
		{ID: 1743, Name: "Ubuntu 22.04 LTS x64", Family: "ubuntu"},
		{ID: 477, Name: "Debian 12 x64", Family: "debian"},
		{ID: 542, Name: "Windows Server 2025", Family: "windows"},
	}}, "")
	if len(catalog.Images) != 1 || catalog.Images[0].Name != "2284" {
		t.Fatalf("unsupported images exposed: %+v", catalog.Images)
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
	if strings.Contains(diagnostics.Err().Error(), secret) || !strings.Contains(diagnostics.Err().Error(), "provider credential validation failed") {
		t.Fatalf("bad diagnostic error: %v", diagnostics.Err())
	}
}

func TestComputeProviderCreateUsesGenericIntentAndDenyPublicPolicy(t *testing.T) {
	var firewallPayload map[string]any
	var firewallRulePayloads []map[string]any
	var serverPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			if err := json.NewDecoder(r.Body).Decode(&firewallPayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			firewallRulePayloads = append(firewallRulePayloads, payload)
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			if err := json.NewDecoder(r.Body).Decode(&serverPayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"active","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:        compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "2284", Labels: map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}},
		BootstrapData: "#cloud-config",
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if record.ID != "abc-123" || record.ProviderState["firewall_group_id"] != "fw-9" || record.PublicIPv4 != "203.0.113.10" {
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
	if serverPayload["region"] != "ewr" || serverPayload["plan"] != "vc2-1c-2gb" || serverPayload["os_id"] != float64(2284) {
		t.Fatalf("bad server payload: %+v", serverPayload)
	}
	if serverPayload["firewall_group_id"] != "fw-9" || serverPayload["enable_ipv6"] != false {
		t.Fatalf("missing ipv4-only firewall attachment: %+v", serverPayload)
	}
	if !payloadHasTags(serverPayload, "managed-by:serverpro", "serverpro-namespace:prod", "serverpro-server:web") {
		t.Fatalf("server ownership tags missing: %+v", serverPayload)
	}
}

func TestComputeProviderCreateStopsBeforeFirewallRulesWhenGroupCheckpointFails(t *testing.T) {
	firewallRulesCreated := false
	instanceCreated := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			firewallRulesCreated = true
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			instanceCreated = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "2284"},
		CheckpointProviderState: func(map[string]string) error {
			return errors.New("checkpoint failed")
		},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "checkpoint failed") {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if firewallRulesCreated || instanceCreated || record.ProviderState["firewall_group_id"] != "fw-9" {
		t.Fatalf("firewallRulesCreated=%t instanceCreated=%t record=%+v", firewallRulesCreated, instanceCreated, record)
	}
}

func TestComputeProviderCreateReturnsFirewallCheckpointOnServerFailure(t *testing.T) {
	ruleRequestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			ruleRequestCount++
			_, _ = w.Write([]byte(`{"firewall_rule":{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/instances":
			http.Error(w, "server create failed", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/instances" && r.URL.Query().Get("label") == "prod-web":
			_, _ = w.Write([]byte(`{"instances":[],"meta":{"links":{"next":"","prev":""}}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "2284"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatal("expected server create failure")
	}
	if record.ID != "" || record.ProviderState["firewall_group_id"] != "fw-9" {
		t.Fatalf("firewall checkpoint missing: %+v", record)
	}
	if ruleRequestCount != 4 {
		t.Fatalf("expected 4 firewall rule requests, got %d", ruleRequestCount)
	}
}

func TestComputeProviderCreateReturnsFirewallCheckpointOnRuleFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/firewalls/fw-9/rules"):
			http.Error(w, "rule create failed", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "vultr", Token: "token"},
		Intent:  compute.ServerIntent{Namespace: "prod", Server: "web", Name: "prod-web", Location: "ewr", Size: "vc2-1c-2gb", Image: "2284"},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatal("expected firewall rule failure")
	}
	if record.ID != "" || record.ProviderState["firewall_group_id"] != "fw-9" {
		t.Fatalf("firewall checkpoint missing: %+v", record)
	}
}

func TestStatusFromInstanceUsesAuthoritativeEmptyLabels(t *testing.T) {
	record := compute.ServerRecord{Labels: map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}}
	status := statusFromInstance(record, Instance{ID: "abc", Label: "prod-web"})
	if len(status.Record.Labels) != 0 {
		t.Fatalf("stale state labels survived empty live tags: %+v", status.Record.Labels)
	}
}

func TestComputeProviderStatusReturnsLiveState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/instances/abc-123" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
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
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" && status == http.StatusOK {
			t.Fatalf("missing auth header: %s", r.Header.Get("Authorization"))
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
			_, _ = w.Write([]byte(`{"os":[{"id":2284,"name":"Ubuntu 24.04 LTS x64","arch":"x64","family":"ubuntu"}],"meta":{"links":{"next":"","prev":""}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}
