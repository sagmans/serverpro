package digitalocean

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
	if p.Name() != "digitalocean" {
		t.Fatalf("name = %q", p.Name())
	}
	caps := p.Capabilities(context.Background())
	if !caps.CreateServer || !caps.DeleteServer || !caps.PowerServer || !caps.Catalog {
		t.Fatalf("capabilities = %+v", caps)
	}
}

func TestComputeProviderDoctorRedactsAndPreservesProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "credential rejected", http.StatusUnauthorized)
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	secret := strings.Repeat("x", 12)
	diagnostics := provider.Doctor(context.Background(), compute.Account{Name: "prod", Provider: "digitalocean", Token: secret})
	var statusErr *httpjson.StatusError
	if diagnostics.Passed() || strings.Contains(diagnostics.Err().Error(), secret) || !strings.Contains(diagnostics.Err().Error(), "provider credential validation failed") || !errors.As(diagnostics.Err(), &statusErr) {
		t.Fatalf("bad diagnostic error: %v", diagnostics.Err())
	}
}

func TestComputeProviderCreateUsesTaggedFirewallAndIPv4OnlyDroplet(t *testing.T) {
	var firewallPayload map[string]any
	var dropletPayload map[string]any
	createdTags := map[string]bool{}
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tags":
			var tagPayload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&tagPayload); err != nil {
				handlerErr.Record(w, "decode tag payload: %v", err)
				return
			}
			createdTags[tagPayload["name"]] = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"tag":{"name":"` + tagPayload["name"] + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			targetTag := firewallTargetTag("prod", "web")
			if !createdTags[targetTag] {
				handlerErr.Record(w, "firewall target tag was not created before firewall: %+v", createdTags)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&firewallPayload); err != nil {
				handlerErr.Record(w, "decode firewall payload: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["serverpro-firewall-target:prod%2Fweb"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			if err := json.NewDecoder(r.Body).Decode(&dropletPayload); err != nil {
				handlerErr.Record(w, "decode droplet payload: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"10.128.0.2","type":"private"},{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"]}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	intent := digitalOceanIntent()
	intent.Labels["environment"] = "production"
	intent.Labels["role"] = "frontend"
	intent.Labels["team"] = "platform"
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:        intent,
		BootstrapData: "#cloud-config",
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if record.ID != "3164444" || record.Account != "prod" || !ok || policyID != "fw-9" || len(record.ProviderState) != 0 || record.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("bad record: %+v", record)
	}
	targetTag := firewallTargetTag("prod", "web")
	expectedTags := append(digitalOceanOwnershipTags("prod", "web"), targetTag, "environment:production", "role:frontend", "team:platform")
	if len(createdTags) != len(expectedTags) {
		t.Fatalf("created tags=%+v want=%+v", createdTags, expectedTags)
	}
	for _, tag := range expectedTags {
		if !createdTags[tag] {
			t.Fatalf("droplet tag %q was not created: %+v", tag, createdTags)
		}
	}
	if firewallPayload["name"] != "prod-web-deny-public" || !payloadHasExactTags(firewallPayload, targetTag) {
		t.Fatalf("bad firewall payload: %+v", firewallPayload)
	}
	assertTailscaleInboundRules(t, firewallPayload)
	assertAllowAllOutboundRules(t, firewallPayload)
	if dropletPayload["region"] != "nyc3" || dropletPayload["size"] != "s-1vcpu-1gb" || dropletPayload["image"] != "ubuntu-24-04-x64" {
		t.Fatalf("bad droplet payload: %+v", dropletPayload)
	}
	if dropletPayload["ipv6"] != false || dropletPayload["user_data"] != "#cloud-config" {
		t.Fatalf("droplet must be IPv4-only with raw user_data: %+v", dropletPayload)
	}
	if !payloadHasTags(dropletPayload, append(digitalOceanOwnershipTags("prod", "web"), targetTag, "environment:production", "role:frontend", "team:platform")...) {
		t.Fatalf("droplet ownership, target, or custom tags missing: %+v", dropletPayload)
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

func TestFirewallTargetTagIsUniquePerManagedServer(t *testing.T) {
	first := firewallTargetTag("prod", "web")
	if first == firewallTargetTag("prod", "api") || first == firewallTargetTag("staging", "web") {
		t.Fatalf("firewall target tags must identify one namespace/server pair: %q", first)
	}
}

func TestComputeProviderCreateReturnsFirewallCheckpointOnDropletFailure(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case handleCreateTag(handlerErr, w, r):
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			http.Error(w, "droplet create failed", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/droplets" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"droplets":[],"links":{"pages":{}},"meta":{"total":0}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
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
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if record.ID != "" || record.Account != "prod" || !ok || policyID != "fw-9" || len(record.ProviderState) != 0 {
		t.Fatalf("firewall checkpoint missing: %+v", record)
	}
}

func TestComputeProviderCreateRejectsUnsafeCheckpointFirewall(t *testing.T) {
	tests := []struct {
		name         string
		firewallJSON string
		wantError    string
	}{
		{
			name:         "foreign identity",
			firewallJSON: `{"firewall":{"id":"fw-9","name":"staging-web-deny-public","tags":["serverpro-firewall-target:b64:cHJvZC93ZWI"],"droplet_ids":[]}}`,
			wantError:    "ownership mismatch",
		},
		{
			name:         "direct attachment",
			firewallJSON: `{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["serverpro-firewall-target:b64:cHJvZC93ZWI"],"droplet_ids":[3164444]}}`,
			wantError:    "attachment",
		},
		{
			name:         "broadened selector",
			firewallJSON: `{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["serverpro-firewall-target:b64:cHJvZC93ZWI"],"droplet_ids":[],"inbound_rules":[{"protocol":"udp","ports":"41641","sources":{"addresses":["0.0.0.0/0","::/0"],"tags":["all-web"]}},{"protocol":"udp","ports":"3478","sources":{"addresses":["0.0.0.0/0","::/0"]}}],"outbound_rules":[{"protocol":"tcp","ports":"0","destinations":{"addresses":["0.0.0.0/0","::/0"]}},{"protocol":"udp","ports":"0","destinations":{"addresses":["0.0.0.0/0","::/0"]}},{"protocol":"icmp","ports":"0","destinations":{"addresses":["0.0.0.0/0","::/0"]}}]}}`,
			wantError:    "unexpected rules",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dropletPosts := 0
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			defer handlerErr.Check()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
					_, _ = w.Write([]byte(test.firewallJSON))
				case r.Method == http.MethodPost && r.URL.Path == "/droplets":
					dropletPosts++
					_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web"}}`))
				default:
					handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
				}
			}))
			defer srv.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
			_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
				Account:          compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
				Intent:           digitalOceanIntent(),
				ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "fw-9"}},
			})
			if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), test.wantError) || dropletPosts != 0 {
				t.Fatalf("diagnostics=%v dropletPosts=%d", diagnostics.Err(), dropletPosts)
			}
		})
	}
}

func TestComputeProviderCreateAcceptsSafeCheckpointFirewall(t *testing.T) {
	firewallGets := 0
	dropletPosts := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			firewallGets++
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["serverpro-firewall-target:b64:cHJvZC93ZWI"],"droplet_ids":[],"inbound_rules":[{"protocol":"udp","ports":"41641","sources":{"addresses":["0.0.0.0/0","::/0"]}},{"protocol":"udp","ports":"3478","sources":{"addresses":["0.0.0.0/0","::/0"]}}],"outbound_rules":[{"protocol":"tcp","ports":"0","destinations":{"addresses":["0.0.0.0/0","::/0"]}},{"protocol":"udp","ports":"0","destinations":{"addresses":["0.0.0.0/0","::/0"]}},{"protocol":"icmp","ports":"0","destinations":{"addresses":["0.0.0.0/0","::/0"]}}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			dropletPosts++
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web"}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:          compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:           digitalOceanIntent(),
		ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "fw-9"}},
	})
	if !diagnostics.Passed() || firewallGets != 1 || dropletPosts != 1 {
		t.Fatalf("diagnostics=%v firewallGets=%d dropletPosts=%d", diagnostics.Err(), firewallGets, dropletPosts)
	}
}

func TestComputeProviderStatusReturnsLiveState(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/droplets/3164444" {
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
			return
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

func handleCreateTag(handlerErr *testhttp.HandlerErrorRecorder, w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.Path != "/tags" {
		return false
	}
	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		handlerErr.Record(w, "decode tag payload: %v", err)
		return true
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

func payloadHasExactTags(payload map[string]any, wants ...string) bool {
	tags, ok := payload["tags"].([]any)
	if !ok || len(tags) != len(wants) {
		return false
	}
	got := make([]string, 0, len(tags))
	for _, tag := range tags {
		value, ok := tag.(string)
		if !ok {
			return false
		}
		got = append(got, value)
	}
	return hasString(got, wants...)
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
	if len(rules) != 2 {
		t.Fatalf("expected 2 inbound tailscale rules, got %+v", rules)
	}
	got := map[string]bool{}
	for _, raw := range rules {
		rule := raw.(map[string]any)
		got[rule["protocol"].(string)+":"+rule["ports"].(string)] = true
	}
	for _, want := range []string{"udp:41641", "udp:3478"} {
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
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case handleCreateTag(handlerErr, w, r):
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			http.Error(w, "droplet create response lost", http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/droplets" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"droplets":[{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}],"links":{"pages":{}},"meta":{"total":1}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:  digitalOceanIntent(),
	})
	policyID, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if diagnostics.Passed() || record.ID != "3164444" || !ok || policyID != "fw-9" || len(record.ProviderState) != 0 {
		t.Fatalf("record=%+v diagnostics=%v", record, diagnostics.Err())
	}
}

func TestComputeProviderPowerUsesNativeActions(t *testing.T) {
	for _, test := range []struct {
		name       string
		action     compute.PowerAction
		wantAction string
	}{
		{name: "start", action: compute.PowerStart, wantAction: "power_on"},
		{name: "stop", action: compute.PowerStop, wantAction: "shutdown"},
		{name: "restart", action: compute.PowerRestart, wantAction: "reboot"},
	} {
		t.Run(test.name, func(t *testing.T) {
			postBody := map[string]any{}
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			defer handlerErr.Check()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
					_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
				case r.Method == http.MethodPost && r.URL.Path == "/droplets/3164444/actions":
					if err := json.NewDecoder(r.Body).Decode(&postBody); err != nil {
						handlerErr.Record(w, "decode action payload: %v", err)
						return
					}
					_, _ = w.Write([]byte(`{"action":{"id":99,"status":"in-progress"}}`))
				default:
					handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
			status, diagnostics := provider.Power(context.Background(), compute.PowerRequest{
				Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
				Record:  compute.ServerRecord{Provider: "digitalocean", Account: "prod", ID: "3164444", Name: "prod-web", Namespace: "prod", Server: "web"},
				Action:  test.action,
			})
			if !diagnostics.Passed() || postBody["type"] != test.wantAction || status.Power != "active" {
				t.Fatalf("diagnostics=%v action=%+v status=%+v", diagnostics.Err(), postBody, status)
			}
		})
	}
}

func TestComputeProviderPowerRejectsOwnershipMismatch(t *testing.T) {
	actionCalled := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:api"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets/3164444/actions":
			actionCalled = true
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
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
