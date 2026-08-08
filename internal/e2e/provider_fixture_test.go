//go:build serverpro_full_chain_e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const fixtureFirewallTargetTagPrefix = "serverpro-firewall-target:"

type providerFixture struct {
	server *httptest.Server
	mu     sync.Mutex
	states map[string]*fixtureState
}

type fixtureState struct {
	serverName   string
	serverLabels map[string]string
	serverTags   []string
	firewallName string
	firewallTags []string
	serverLive   bool
	firewallLive bool
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	fixture := &providerFixture{states: map[string]*fixtureState{}}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *providerFixture) URL() string { return f.server.URL }

func (f *providerFixture) useLegacyDigitalOceanFirewall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.states["digitalocean"]
	if state == nil {
		return
	}
	legacyTags := make([]string, 0, len(state.serverTags))
	for _, tag := range state.serverTags {
		if !strings.HasPrefix(tag, fixtureFirewallTargetTagPrefix) {
			legacyTags = append(legacyTags, tag)
		}
	}
	state.serverTags = legacyTags
	state.firewallTags = append([]string(nil), legacyTags...)
}

func (f *providerFixture) resourceCount(provider string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.states[provider]
	if state == nil {
		return 0
	}
	count := 0
	if state.serverLive {
		count++
	}
	if state.firewallLive {
		count++
	}
	return count
}

func (f *providerFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	provider, path, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !ok {
		http.Error(w, "provider prefix required", http.StatusNotFound)
		return
	}
	path = "/" + path
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.states[provider]
	if state == nil {
		state = &fixtureState{}
		f.states[provider] = state
	}

	var handled bool
	switch provider {
	case "hetzner":
		handled = serveHetzner(w, r, path, state)
	case "vultr":
		handled = serveVultr(w, r, path, state)
	case "digitalocean":
		handled = serveDigitalOcean(w, r, path, state)
	}
	if !handled {
		http.Error(w, fmt.Sprintf("unexpected request %s %s", r.Method, r.URL.String()), http.StatusNotFound)
	}
}

func serveHetzner(w http.ResponseWriter, r *http.Request, path string, state *fixtureState) bool {
	switch {
	case r.Method == http.MethodGet && path == "/locations":
		writeJSONResponse(w, map[string]any{"locations": []map[string]any{{"name": "fsn1"}}})
	case r.Method == http.MethodPost && path == "/firewalls":
		body := decodeBody(r)
		state.firewallName = stringValue(body["name"])
		state.serverLabels = stringMap(body["labels"])
		state.firewallLive = true
		writeJSONResponse(w, map[string]any{"firewall": hetznerFirewall(state)})
	case r.Method == http.MethodPost && path == "/servers":
		body := decodeBody(r)
		state.serverName = stringValue(body["name"])
		state.serverLabels = stringMap(body["labels"])
		state.serverLive = true
		writeJSONResponse(w, map[string]any{"server": hetznerServer(state), "action": map[string]any{"id": 7, "status": "success"}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/actions/"):
		writeJSONResponse(w, map[string]any{"action": map[string]any{"id": 7, "status": "success"}})
	case r.Method == http.MethodGet && path == "/servers/42" && state.serverLive:
		writeJSONResponse(w, map[string]any{"server": hetznerServer(state)})
	case r.Method == http.MethodDelete && path == "/servers/42" && state.serverLive:
		state.serverLive = false
		writeJSONResponse(w, map[string]any{"action": map[string]any{"id": 8, "status": "success"}})
	case r.Method == http.MethodGet && path == "/firewalls/9" && state.firewallLive:
		writeJSONResponse(w, map[string]any{"firewall": hetznerFirewall(state)})
	case r.Method == http.MethodDelete && path == "/firewalls/9" && state.firewallLive:
		state.firewallLive = false
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func hetznerServer(state *fixtureState) map[string]any {
	return map[string]any{
		"id": 42, "name": state.serverName, "status": "running", "labels": state.serverLabels,
		"public_net": map[string]any{"ipv4": map[string]string{"ip": "203.0.113.10"}, "ipv6": map[string]string{"ip": ""}},
		"location":   map[string]string{"name": "fsn1"}, "server_type": map[string]string{"name": "cx23"},
		"image": map[string]string{"name": "ubuntu-24.04"},
	}
}

func hetznerFirewall(state *fixtureState) map[string]any {
	return map[string]any{"id": 9, "name": state.firewallName, "labels": state.serverLabels}
}

func serveVultr(w http.ResponseWriter, r *http.Request, path string, state *fixtureState) bool {
	switch {
	case r.Method == http.MethodGet && path == "/regions":
		writeJSONResponse(w, map[string]any{"regions": []map[string]any{{"id": "ewr"}}, "meta": map[string]any{}})
	case r.Method == http.MethodPost && path == "/firewalls":
		body := decodeBody(r)
		state.firewallName = stringValue(body["description"])
		state.firewallLive = true
		writeJSONResponse(w, map[string]any{"firewall_group": vultrFirewall(state)})
	case r.Method == http.MethodGet && path == "/firewalls/fw-9/rules":
		writeJSONResponse(w, map[string]any{"firewall_rules": []any{}, "meta": map[string]any{"links": map[string]string{}}})
	case r.Method == http.MethodPost && path == "/firewalls/fw-9/rules":
		writeJSONResponse(w, map[string]any{"firewall_rule": map[string]any{"id": 1}})
	case r.Method == http.MethodPost && path == "/instances":
		body := decodeBody(r)
		state.serverName = stringValue(body["label"])
		state.serverTags = stringSlice(body["tags"])
		state.serverLive = true
		writeJSONResponse(w, map[string]any{"instance": vultrInstance(state)})
	case r.Method == http.MethodGet && path == "/instances" && state.serverLive:
		writeJSONResponse(w, map[string]any{"instances": []any{vultrInstance(state)}, "meta": map[string]any{"links": map[string]string{}}})
	case r.Method == http.MethodGet && path == "/instances/instance-42" && state.serverLive:
		writeJSONResponse(w, map[string]any{"instance": vultrInstance(state)})
	case r.Method == http.MethodDelete && path == "/instances/instance-42" && state.serverLive:
		state.serverLive = false
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && path == "/firewalls/fw-9" && state.firewallLive:
		writeJSONResponse(w, map[string]any{"firewall_group": vultrFirewall(state)})
	case r.Method == http.MethodDelete && path == "/firewalls/fw-9" && state.firewallLive:
		state.firewallLive = false
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func vultrInstance(state *fixtureState) map[string]any {
	return map[string]any{
		"id": "instance-42", "label": state.serverName, "hostname": state.serverName,
		"region": "ewr", "plan": "vc2-1c-1gb", "os_id": 1743, "status": "active",
		"power_status": "running", "main_ip": "203.0.113.20", "tags": state.serverTags,
		"firewall_group_id": "fw-9",
	}
}

func vultrFirewall(state *fixtureState) map[string]any {
	return map[string]any{"id": "fw-9", "description": state.firewallName}
}

func serveDigitalOcean(w http.ResponseWriter, r *http.Request, path string, state *fixtureState) bool {
	switch {
	case r.Method == http.MethodGet && path == "/regions":
		writeJSONResponse(w, map[string]any{"regions": []map[string]any{{"slug": "nyc3", "available": true}}})
	case r.Method == http.MethodPost && path == "/tags":
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPost && path == "/firewalls":
		body := decodeBody(r)
		state.firewallName = stringValue(body["name"])
		state.firewallTags = stringSlice(body["tags"])
		state.firewallLive = true
		writeJSONResponse(w, map[string]any{"firewall": digitalOceanFirewall(state)})
	case r.Method == http.MethodPost && path == "/droplets":
		body := decodeBody(r)
		state.serverName = stringValue(body["name"])
		state.serverTags = stringSlice(body["tags"])
		state.serverLive = true
		writeJSONResponse(w, map[string]any{"droplet": digitalOceanDroplet(state)})
	case r.Method == http.MethodGet && path == "/droplets" && state.serverLive:
		writeJSONResponse(w, map[string]any{"droplets": []any{digitalOceanDroplet(state)}, "links": map[string]any{}})
	case r.Method == http.MethodGet && path == "/firewalls" && state.firewallLive:
		writeJSONResponse(w, map[string]any{"firewalls": []any{digitalOceanFirewall(state)}, "links": map[string]any{}})
	case r.Method == http.MethodGet && path == "/droplets/42" && state.serverLive:
		writeJSONResponse(w, map[string]any{"droplet": digitalOceanDroplet(state)})
	case r.Method == http.MethodDelete && path == "/droplets/42" && state.serverLive:
		state.serverLive = false
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && path == "/firewalls/fw-9" && state.firewallLive:
		writeJSONResponse(w, map[string]any{"firewall": digitalOceanFirewall(state)})
	case r.Method == http.MethodDelete && path == "/firewalls/fw-9" && state.firewallLive:
		state.firewallLive = false
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func digitalOceanDroplet(state *fixtureState) map[string]any {
	return map[string]any{
		"id": 42, "name": state.serverName, "status": "active", "tags": state.serverTags,
		"networks": map[string]any{"v4": []map[string]string{{"ip_address": "203.0.113.30", "type": "public"}}, "v6": []any{}},
	}
}

func digitalOceanFirewall(state *fixtureState) map[string]any {
	return map[string]any{"id": "fw-9", "name": state.firewallName, "tags": state.firewallTags, "status": "succeeded"}
}

func decodeBody(r *http.Request) map[string]any {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringMap(value any) map[string]string {
	raw, _ := value.(map[string]any)
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		out[key] = stringValue(value)
	}
	return out
}

func stringSlice(value any) []string {
	raw, _ := value.([]any)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		out = append(out, stringValue(value))
	}
	return out
}

func writeJSONResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}
