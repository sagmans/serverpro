package digitalocean

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateFirewallSendsTaggedTailscaleAndOutboundRules(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["serverpro.server:web"]}}`))
	}))
	defer ts.Close()

	fw, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateFirewall(context.Background(), CreateFirewallInput{
		Name: "prod-web-deny-public",
		Tags: []string{"managed-by:serverpro", "serverpro.namespace:prod", "serverpro.server:web"},
	})
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if fw.ID != "fw-9" || fw.Name != "prod-web-deny-public" {
		t.Fatalf("bad firewall: %+v", fw)
	}
	assertTailscaleInboundRules(t, got)
	assertAllowAllOutboundRules(t, got)
}

func TestCreateDropletSendsIPv4OnlyAndRawUserData(t *testing.T) {
	var got map[string]any
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/droplets" {
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
		_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"new","networks":{"v4":[],"v6":[]},"tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
	}))
	defer ts.Close()

	droplet, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateDroplet(context.Background(), CreateDropletInput{
		Name:     "prod-web",
		Region:   "nyc3",
		Size:     "s-1vcpu-1gb",
		Image:    "ubuntu-24-04-x64",
		Tags:     []string{"managed-by:serverpro", "serverpro.namespace:prod", "serverpro.server:web"},
		UserData: "#cloud-config",
	})
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if droplet.ID != 3164444 || droplet.Name != "prod-web" {
		t.Fatalf("bad droplet: %+v", droplet)
	}
	if got["region"] != "nyc3" || got["size"] != "s-1vcpu-1gb" || got["image"] != "ubuntu-24-04-x64" {
		t.Fatalf("bad base payload: %+v", got)
	}
	if got["ipv6"] != false || got["user_data"] != "#cloud-config" {
		t.Fatalf("expected ipv4-only raw user_data: %+v", got)
	}
}

func TestGetDropletDecodesPublicIPv4StatusAndTags(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/droplets/3164444" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","networks":{"v4":[{"ip_address":"10.128.0.2","type":"private"},{"ip_address":"203.0.113.10","type":"public"}],"v6":[]},"tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
	}))
	defer ts.Close()

	droplet, err := NewWithHTTP("token", ts.URL, ts.Client()).GetDroplet(context.Background(), 3164444)
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if droplet.Status != "active" || publicIPv4(droplet) != "203.0.113.10" || len(droplet.Tags) != 3 {
		t.Fatalf("bad droplet: %+v", droplet)
	}
}

func TestFindDropletByNameFiltersAndReturnsSingleMatch(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/droplets" || r.URL.Query().Get("name") != "prod-web" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.String())
			return
		}
		_, _ = w.Write([]byte(`{"droplets":[{"id":3164444,"name":"prod-web"},{"id":3164445,"name":"other"}],"links":{"pages":{}},"meta":{"total":2}}`))
	}))
	defer ts.Close()

	droplet, err := NewWithHTTP("token", ts.URL, ts.Client()).FindDropletByName(context.Background(), "prod-web")
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if droplet.ID != 3164444 {
		t.Fatalf("bad droplet: %+v", droplet)
	}
}

func TestFindDropletByNameFollowsPagination(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/droplets" || r.URL.Query().Get("name") != "prod-web" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.String())
			return
		}
		switch r.URL.Query().Get("page") {
		case "", "1":
			_, _ = w.Write([]byte(`{"droplets":[{"id":3164445,"name":"other"}],"links":{"pages":{"next":"https://api.digitalocean.com/v2/droplets?page=2"}},"meta":{"total":2}}`))
		case "2":
			_, _ = w.Write([]byte(`{"droplets":[{"id":3164444,"name":"prod-web"}],"links":{"pages":{}},"meta":{"total":2}}`))
		default:
			handlerErr.record(w, "unexpected page %s", r.URL.Query().Get("page"))
		}
	}))
	defer ts.Close()

	droplet, err := NewWithHTTP("token", ts.URL, ts.Client()).FindDropletByName(context.Background(), "prod-web")
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if droplet.ID != 3164444 {
		t.Fatalf("bad droplet: %+v", droplet)
	}
}

func TestDeleteDropletAndFirewallCallEndpoints(t *testing.T) {
	called := map[string]bool{}
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			called["droplet"] = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
			called["firewall"] = true
			w.WriteHeader(http.StatusNoContent)
		default:
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", ts.URL, ts.Client())
	if err := client.DeleteDroplet(context.Background(), 3164444); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteFirewall(context.Background(), "fw-9"); err != nil {
		t.Fatal(err)
	}
	handlerErr.check()
	if !called["droplet"] || !called["firewall"] {
		t.Fatalf("delete calls missing: %+v", called)
	}
}

func TestPowerEndpointsUseDigitalOceanActions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method func(Client, context.Context, int64) error
		action string
	}{
		{name: "start", method: Client.PowerOnDroplet, action: "power_on"},
		{name: "stop", method: Client.ShutdownDroplet, action: "shutdown"},
		{name: "restart", method: Client.RebootDroplet, action: "reboot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			handlerErr := newHandlerErrorRecorder(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/droplets/3164444/actions" {
					handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					handlerErr.record(w, "decode request: %v", err)
					return
				}
				_, _ = w.Write([]byte(`{"action":{"id":99,"status":"in-progress"}}`))
			}))
			defer ts.Close()

			if err := tc.method(NewWithHTTP("token", ts.URL, ts.Client()), context.Background(), 3164444); err != nil {
				t.Fatal(err)
			}
			handlerErr.check()
			if got["type"] != tc.action {
				t.Fatalf("action = %q, want %q", got["type"], tc.action)
			}
		})
	}
}
