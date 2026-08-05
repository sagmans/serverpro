package digitalocean

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
	if !provider.SupportsImageReference("ubuntu-24-04-x64") || provider.SupportsImageReference("ubuntu-22-04-x64") {
		t.Fatal("provider image policy must match Ubuntu 24.04 exactly")
	}
}

func TestComputeProviderNameAndCapabilities(t *testing.T) {
	p := NewComputeProvider(nil)
	if p.Name() != "digitalocean" {
		t.Fatalf("name = %q", p.Name())
	}
	caps := p.Capabilities(context.Background())
	if !caps.CreateServer || !caps.DeleteServer || !caps.PowerServer || !caps.Catalog {
		t.Fatalf("capabilities = %+v", caps)
	}
}

func TestComputeProviderCreateUsesTaggedFirewallAndIPv4OnlyDroplet(t *testing.T) {
	var firewallPayload map[string]any
	var dropletPayload map[string]any
	createdTags := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tags":
			var tagPayload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&tagPayload); err != nil {
				t.Fatal(err)
			}
			createdTags[tagPayload["name"]] = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"tag":{"name":"` + tagPayload["name"] + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			for _, tag := range digitalOceanOwnershipTags("prod", "web") {
				if !createdTags[tag] {
					t.Fatalf("firewall created before tag %q", tag)
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&firewallPayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			if err := json.NewDecoder(r.Body).Decode(&dropletPayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"10.128.0.2","type":"private"},{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"]}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:        digitalOceanIntent(),
		BootstrapData: "#cloud-config",
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if record.ID != "3164444" || record.Account != "prod" || record.ProviderState["firewall_id"] != "fw-9" || record.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("bad record: %+v", record)
	}
	for _, tag := range digitalOceanOwnershipTags("prod", "web") {
		if !createdTags[tag] {
			t.Fatalf("ownership tag %q was not created", tag)
		}
	}
	if firewallPayload["name"] != "prod-web-deny-public" {
		t.Fatalf("bad firewall payload: %+v", firewallPayload)
	}
	if !payloadHasTags(firewallPayload, digitalOceanOwnershipTags("prod", "web")...) {
		t.Fatalf("firewall ownership tags missing: %+v", firewallPayload)
	}
	assertTailscaleInboundRules(t, firewallPayload)
	assertAllowAllOutboundRules(t, firewallPayload)
	if dropletPayload["region"] != "nyc3" || dropletPayload["size"] != "s-1vcpu-1gb" || dropletPayload["image"] != "ubuntu-24-04-x64" {
		t.Fatalf("bad droplet payload: %+v", dropletPayload)
	}
	if dropletPayload["ipv6"] != false || dropletPayload["user_data"] != "#cloud-config" {
		t.Fatalf("droplet must be IPv4-only with raw user_data: %+v", dropletPayload)
	}
	if !payloadHasTags(dropletPayload, digitalOceanOwnershipTags("prod", "web")...) {
		t.Fatalf("droplet ownership tags missing: %+v", dropletPayload)
	}
}

func TestComputeProviderCreateStopsBeforeDropletWhenFirewallCheckpointFails(t *testing.T) {
	dropletCreated := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case handleCreateTag(t, w, r):
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			dropletCreated = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:  digitalOceanIntent(),
		CheckpointProviderState: func(map[string]string) error {
			return errors.New("checkpoint failed")
		},
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "checkpoint failed") {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if dropletCreated || record.ProviderState["firewall_id"] != "fw-9" {
		t.Fatalf("dropletCreated=%t record=%+v", dropletCreated, record)
	}
}

func TestLabelsToTagsUsesDigitalOceanSafeOwnershipTags(t *testing.T) {
	labels := map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}
	tags := labelsToTags(labels)
	for _, tag := range tags {
		if strings.Contains(tag, ".") {
			t.Fatalf("DigitalOcean tag contains invalid dot: %q in %+v", tag, tags)
		}
	}
	if !hasString(tags, digitalOceanOwnershipTags("prod", "web")...) {
		t.Fatalf("ownership tags missing: %+v", tags)
	}
	decoded := tagsToLabels(tags)
	if decoded["managed-by"] != "serverpro" || decoded["serverpro.namespace"] != "prod" || decoded["serverpro.server"] != "web" {
		t.Fatalf("ownership tags did not decode: %+v", decoded)
	}
}

func TestComputeProviderCreateReturnsFirewallCheckpointOnDropletFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case handleCreateTag(t, w, r):
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			http.Error(w, "droplet create failed", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/droplets" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"droplets":[],"links":{"pages":{}},"meta":{"total":0}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:  digitalOceanIntent(),
	})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatal("expected droplet create failure")
	}
	if record.ID != "" || record.Account != "prod" || record.ProviderState["firewall_id"] != "fw-9" {
		t.Fatalf("firewall checkpoint missing: %+v", record)
	}
}

func TestStatusFromDropletUsesAuthoritativeEmptyLabels(t *testing.T) {
	record := compute.ServerRecord{Labels: map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"}}
	status := statusFromDroplet(record, Droplet{ID: 1, Name: "prod-web"})
	if len(status.Record.Labels) != 0 {
		t.Fatalf("stale state labels survived empty live tags: %+v", status.Record.Labels)
	}
}

func TestComputeProviderStatusReturnsLiveState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/droplets/3164444" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	status, diagnostics := provider.Status(context.Background(), compute.ServerRef{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  compute.ServerRecord{Provider: "digitalocean", ID: "3164444", Name: "prod-web", Namespace: "prod", Server: "web"},
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if status.Power != "active" || status.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("bad status: %+v", status)
	}
}

func digitalOceanIntent() compute.ServerIntent {
	return compute.ServerIntent{
		Namespace: "prod",
		Server:    "web",
		Name:      "prod-web",
		Location:  "nyc3",
		Size:      "s-1vcpu-1gb",
		Image:     "ubuntu-24-04-x64",
		Labels:    map[string]string{"managed-by": "serverpro", "serverpro.namespace": "prod", "serverpro.server": "web"},
	}
}

func handleCreateTag(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	if r.Method != http.MethodPost || r.URL.Path != "/tags" {
		return false
	}
	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"tag":{"name":"` + payload["name"] + `"}}`))
	return true
}

func digitalOceanOwnershipTags(namespace, server string) []string {
	return []string{"managed-by:serverpro", "serverpro-namespace:" + namespace, "serverpro-server:" + server}
}

func hasString(items []string, wants ...string) bool {
	got := map[string]bool{}
	for _, item := range items {
		got[item] = true
	}
	for _, want := range wants {
		if !got[want] {
			return false
		}
	}
	return true
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

func assertTailscaleInboundRules(t *testing.T, payload map[string]any) {
	t.Helper()
	rules := payload["inbound_rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("expected 1 inbound tailscale rule, got %+v", rules)
	}
	got := map[string]bool{}
	for _, raw := range rules {
		rule := raw.(map[string]any)
		got[rule["protocol"].(string)+":"+rule["ports"].(string)] = true
	}
	for _, want := range []string{"udp:41641"} {
		if !got[want] {
			t.Fatalf("missing inbound %s in %+v", want, rules)
		}
	}
}

func assertAllowAllOutboundRules(t *testing.T, payload map[string]any) {
	t.Helper()
	rules := payload["outbound_rules"].([]any)
	if len(rules) != 3 {
		t.Fatalf("expected 3 outbound rules, got %+v", rules)
	}
	got := map[string]bool{}
	for _, raw := range rules {
		rule := raw.(map[string]any)
		got[rule["protocol"].(string)+":"+rule["ports"].(string)] = true
	}
	for _, want := range []string{"tcp:0", "udp:0", "icmp:0"} {
		if !got[want] {
			t.Fatalf("missing outbound %s in %+v", want, rules)
		}
	}
}

func TestComputeProviderCreateReconcilesDropletByNameOnCreateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case handleCreateTag(t, w, r):
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			http.Error(w, "droplet create response lost", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/droplets" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"droplets":[{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}],"links":{"pages":{}},"meta":{"total":1}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:  digitalOceanIntent(),
	})
	if diagnostics.Passed() || record.ID != "3164444" || record.ProviderState["firewall_id"] != "fw-9" {
		t.Fatalf("record=%+v diagnostics=%v", record, diagnostics.Err())
	}
}

func TestComputeProviderRestartUsesNativeRebootAction(t *testing.T) {
	postBody := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets/3164444/actions":
			if err := json.NewDecoder(r.Body).Decode(&postBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"action":{"id":99,"status":"in-progress","type":"reboot"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	status, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  compute.ServerRecord{Provider: "digitalocean", Account: "prod", ID: "3164444", Name: "prod-web", Namespace: "prod", Server: "web"},
		Action:  compute.PowerRestart,
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	if postBody["type"] != "reboot" || status.Power != "active" {
		t.Fatalf("action=%+v status=%+v", postBody, status)
	}
}

func TestComputeProviderPowerRejectsOwnershipMismatch(t *testing.T) {
	actionCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:api"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets/3164444/actions":
			actionCalled = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  compute.ServerRecord{Provider: "digitalocean", Account: "prod", ID: "3164444", Name: "prod-web", Namespace: "prod", Server: "web"},
		Action:  compute.PowerStart,
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership mismatch") || actionCalled {
		t.Fatalf("diagnostics=%v actionCalled=%t", diagnostics.Err(), actionCalled)
	}
}
