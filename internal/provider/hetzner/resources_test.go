package hetzner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestCreateServerDisablesPublicIPv6(t *testing.T) {
	var got map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/servers" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.Record(w, "decode request: %v", err)
			return
		}
		_, _ = w.Write([]byte(`{"server":{"id":42,"name":"prod-web","public_net":{"ipv4":{"ip":"192.0.2.10"}}},"action":{"id":77}}`))
	}))
	defer ts.Close()

	input := CreateServerInput{Name: "prod-web", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"}
	srv, actionID, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateServer(context.Background(), input, 99, "#cloud-config")
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if srv.ID != 42 || actionID != 77 {
		t.Fatalf("server/action = %+v/%d", srv, actionID)
	}
	publicNet, ok := got["public_net"].(map[string]any)
	if !ok {
		t.Fatalf("missing public_net payload: %+v", got)
	}
	if publicNet["enable_ipv4"] != true || publicNet["enable_ipv6"] != false {
		t.Fatalf("bad public_net payload: %+v", publicNet)
	}
}

func TestCreateFirewallSendsEmptyRulesAndLabels(t *testing.T) {
	var got map[string]any
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/firewalls" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			handlerErr.Record(w, "decode request: %v", err)
			return
		}
		_, _ = w.Write([]byte(`{"firewall":{"id":99,"name":"prod-fw","labels":{"project":"prod"}}}`))
	}))
	defer ts.Close()

	fw, err := NewWithHTTP("token", ts.URL, ts.Client()).CreateFirewall(context.Background(), "prod-fw", map[string]string{"project": "prod"})
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if fw.ID != 99 || fw.Name != "prod-fw" || fw.Labels["project"] != "prod" {
		t.Fatalf("firewall = %+v", fw)
	}
	if got["name"] != "prod-fw" || got["labels"].(map[string]any)["project"] != "prod" {
		t.Fatalf("bad payload: %+v", got)
	}
	if rules, ok := got["rules"].([]any); !ok || len(rules) != 0 {
		t.Fatalf("rules = %#v", got["rules"])
	}
}
