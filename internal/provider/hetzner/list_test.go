package hetzner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestListServersPagesAndMapsOwnership(t *testing.T) {
	page := 0
	firewallPages := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		switch r.URL.Path {
		case "/servers":
			page++
			if page == 1 {
				_, _ = w.Write([]byte(`{"servers":[{"id":1,"name":"demo-web","status":"running","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"},"public_net":{"ipv4":{"ip":"203.0.113.10"},"ipv6":{"ip":""}},"server_type":{"name":"cx23"},"image":{"name":"ubuntu-24.04"},"location":{"name":"fsn1"}}],"meta":{"pagination":{"next_page":2}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"servers":[{"id":2,"name":"other","status":"off","labels":{},"public_net":{"ipv4":{"ip":"203.0.113.11"},"ipv6":{"ip":""}},"server_type":{"name":"cx23"},"image":{"name":"ubuntu-24.04"},"location":{"name":"nbg1"}}],"meta":{"pagination":{"next_page":null}}}`))
		case "/firewalls":
			firewallPages++
			if firewallPages == 1 {
				_, _ = w.Write([]byte(`{"firewalls":[{"id":8,"name":"other-deny-public","labels":{}}],"meta":{"pagination":{"next_page":2}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"firewalls":[{"id":9,"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}],"meta":{"pagination":{"next_page":null}}}`))
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	servers, err := NewWithHTTP("token", ts.URL, ts.Client()).ListServers(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].ID != 1 || servers[1].ID != 2 {
		t.Fatalf("servers=%+v", servers)
	}

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	// Reset server for second list via provider (new connection path).
	page = 0
	records, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	handlerErr.Check()
	if err := diagnostics.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%+v", records)
	}
	namespace, server, ok := ownership.OwnershipFromLabels(records[0].Labels)
	policyID, found := compute.ManagedResourceID(records[0].ManagedResources, compute.ManagedResourceAccessPolicy)
	if !ok || namespace != "demo" || server != "web" || records[0].PublicIPv4 != "203.0.113.10" || !found || policyID != "9" || firewallPages != 2 {
		t.Fatalf("record0=%+v ns=%s server=%s ok=%t firewallPages=%d", records[0], namespace, server, ok, firewallPages)
	}
}

func TestListRejectsIncompleteManagedAccessPolicyRecovery(t *testing.T) {
	for _, test := range []struct {
		name      string
		firewalls string
		want      string
	}{
		{name: "missing", firewalls: `{"firewalls":[],"meta":{"pagination":{"next_page":null}}}`, want: "not found"},
		{name: "ambiguous", firewalls: `{"firewalls":[{"id":9,"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}},{"id":10,"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}],"meta":{"pagination":{"next_page":null}}}`, want: "ambiguous"},
		{name: "missing id", firewalls: `{"firewalls":[{"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}],"meta":{"pagination":{"next_page":null}}}`, want: "id missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/servers":
					_, _ = w.Write([]byte(`{"servers":[{"id":1,"name":"demo-web","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}],"meta":{"pagination":{"next_page":null}}}`))
				case "/firewalls":
					_, _ = w.Write([]byte(test.firewalls))
				default:
					handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer ts.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
			_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
			handlerErr.Check()
			if err := diagnostics.Err(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("diagnostics=%v", err)
			}
		})
	}
}

func TestListSkipsAccessPolicyInventoryWithoutManagedServers(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/servers" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"servers":[{"id":1,"name":"manual","labels":{}}],"meta":{"pagination":{"next_page":null}}}`))
	}))
	defer ts.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	records, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	handlerErr.Check()
	if err := diagnostics.Err(); err != nil || len(records) != 1 || len(records[0].ManagedResources) != 0 {
		t.Fatalf("records=%+v diagnostics=%v", records, err)
	}
}

func TestListRedactsAccessPolicyInventoryFailure(t *testing.T) {
	secret := "access-policy-list-secret"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/servers" {
			_, _ = w.Write([]byte(`{"servers":[{"id":1,"name":"demo-web","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}],"meta":{"pagination":{"next_page":null}}}`))
			return
		}
		http.Error(w, secret, http.StatusForbidden)
	}))
	defer ts.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: secret}})
	if err := diagnostics.Err(); err == nil || !strings.Contains(err.Error(), "provider access policy list failed") || strings.Contains(err.Error(), secret) {
		t.Fatalf("diagnostics=%v", err)
	}
}
