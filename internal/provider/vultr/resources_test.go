package vultr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateInstanceSendsIPv4OnlyAndBase64UserData(t *testing.T) {
	var got map[string]any
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/instances" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			handlerErr.record(w, "missing auth header")
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.record(w, "decode request: %v", err)
			return
		}
		_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"pending","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
	}))
	defer ts.Close()

	input := CreateInstanceInput{
		Region:          "ewr",
		Plan:            "vc2-1c-2gb",
		OSID:            2284,
		Label:           "prod-web",
		Hostname:        "prod-web",
		Tags:            []string{"managed-by:serverpro", "serverpro.namespace:prod", "serverpro.server:web"},
		FirewallGroupID: "fw-1",
		UserData:        "#cloud-config",
	}
	inst, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateInstance(context.Background(), input)
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if inst.ID != "abc-123" || inst.MainIP != "203.0.113.10" {
		t.Fatalf("bad response: %+v", inst)
	}
	if got["region"] != "ewr" || got["plan"] != "vc2-1c-2gb" || got["os_id"] != float64(2284) {
		t.Fatalf("bad base payload: %+v", got)
	}
	if got["enable_ipv6"] != false {
		t.Fatalf("expected enable_ipv6=false: %+v", got)
	}
	if got["firewall_group_id"] != "fw-1" {
		t.Fatalf("missing firewall_group_id: %+v", got)
	}
	encoded, _ := got["user_data"].(string)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(decoded) != "#cloud-config" {
		t.Fatalf("user_data not base64 encoded: %q", encoded)
	}
	tags := got["tags"].([]any)
	if len(tags) != 3 {
		t.Fatalf("bad tags: %+v", tags)
	}
}

func TestCreateFirewallGroupSendsDescription(t *testing.T) {
	var got map[string]any
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/firewalls" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.record(w, "decode request: %v", err)
			return
		}
		_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-9","description":"prod-web-deny-public"}}`))
	}))
	defer ts.Close()

	fw, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateFirewallGroup(context.Background(), "prod-web-deny-public")
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if fw.ID != "fw-9" || fw.Description != "prod-web-deny-public" {
		t.Fatalf("bad firewall group: %+v", fw)
	}
	if got["description"] != "prod-web-deny-public" {
		t.Fatalf("bad payload: %+v", got)
	}
}

func TestCreateFirewallRuleSendsIPTypeProtocolSubnetAndPort(t *testing.T) {
	var got map[string]any
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/firewalls/fw-9/rules" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.record(w, "decode request: %v", err)
			return
		}
		_, _ = w.Write([]byte(`{"firewall_rule":{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}}`))
	}))
	defer ts.Close()

	rule, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateFirewallRule(context.Background(), "fw-9", CreateFirewallRuleInput{
		IPType:     "v4",
		Protocol:   "udp",
		Port:       "41641",
		Subnet:     "0.0.0.0",
		SubnetSize: 0,
		Notes:      "tailscale wireguard",
	})
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID != 1 || rule.Protocol != "udp" || rule.Port != "41641" {
		t.Fatalf("bad firewall rule: %+v", rule)
	}
	if got["ip_type"] != "v4" || got["protocol"] != "udp" || got["port"] != "41641" || got["subnet"] != "0.0.0.0" || got["subnet_size"] != float64(0) {
		t.Fatalf("bad payload: %+v", got)
	}
}

func TestFirewallRulesListsAllPages(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/firewalls/fw-9/rules" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		requests++
		if r.URL.Query().Get("cursor") == "page-2" {
			_, _ = w.Write([]byte(`{"firewall_rules":[{"id":2,"action":"accept","ip_type":"v6","protocol":"udp","port":"41641","subnet":"::","subnet_size":0}],"meta":{"links":{"next":"","prev":""}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"firewall_rules":[{"id":1,"action":"accept","ip_type":"v4","protocol":"udp","port":"41641","subnet":"0.0.0.0","subnet_size":0}],"meta":{"links":{"next":"page-2","prev":""}}}`))
	}))
	defer ts.Close()

	rules, err := NewWithHTTP("token", ts.URL, ts.Client()).FirewallRules(context.Background(), "fw-9")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(rules) != 2 || rules[1].IPType != "v6" {
		t.Fatalf("requests=%d rules=%+v", requests, rules)
	}
}

func TestGetInstanceDecodesStatusAndTags(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/instances/abc-123" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"instance":{"id":"abc-123","label":"prod-web","main_ip":"203.0.113.10","status":"active","power_status":"running","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
	}))
	defer ts.Close()

	inst, err := NewWithHTTP("token", ts.URL, ts.Client()).GetInstance(context.Background(), "abc-123")
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != "active" || inst.PowerStatus != "running" || inst.MainIP != "203.0.113.10" {
		t.Fatalf("bad instance: %+v", inst)
	}
}

func TestFindInstanceByLabelFiltersAndReturnsSingleMatch(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/instances" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"instances":[{"id":"abc-123","label":"prod-web"},{"id":"other","label":"other"}],"meta":{"links":{"next":"","prev":""}}}`))
	}))
	defer ts.Close()

	inst, err := NewWithHTTP("token", ts.URL, ts.Client()).FindInstanceByLabel(context.Background(), "prod-web")
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if inst.ID != "abc-123" {
		t.Fatalf("bad instance: %+v", inst)
	}
}

func TestDeleteInstanceCallsEndpoint(t *testing.T) {
	called := false
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/instances/abc-123" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	if err := NewWithHTTP("token", ts.URL, ts.Client()).DeleteInstance(context.Background(), "abc-123"); err != nil {
		t.Fatal(err)
	}
	handlerErr.check()
	if !called {
		t.Fatal("delete not called")
	}
}

func TestPowerEndpointsMapToVultrActions(t *testing.T) {
	for _, action := range []string{"start", "halt", "reboot"} {
		t.Run(action, func(t *testing.T) {
			handlerErr := newHandlerErrorRecorder(t)
			called := false
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/instances/abc-123/"+action {
					handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
					return
				}
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			defer ts.Close()

			client := NewWithHTTP("token", ts.URL, ts.Client())
			var err error
			switch action {
			case "start":
				err = client.StartInstance(context.Background(), "abc-123")
			case "halt":
				err = client.HaltInstance(context.Background(), "abc-123")
			case "reboot":
				err = client.RebootInstance(context.Background(), "abc-123")
			}
			if err != nil {
				t.Fatal(err)
			}
			handlerErr.check()
			if !called {
				t.Fatalf("%s not called", action)
			}
		})
	}
}
