package vultr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
)

func TestComputeProviderListRejectsMissingManagedRecoveryMetadata(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/instances":
			_, _ = w.Write([]byte(`{"instances":[{"id":"abc","label":"demo-dev","plan":"vc2-1c-1gb","os_id":2284,"firewall_group_id":"fw-1","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:dev"]}],"meta":{"links":{"next":"","prev":""}}}`))
		case "/firewalls/fw-1":
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-1","description":"demo-dev-deny-public"}}`))
		default:
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	handlerErr.check()
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "metadata missing") {
		t.Fatalf("expected missing region metadata failure, got %v", diagnostics.Err())
	}
}

func TestComputeProviderListRequiresOwnedManagedFirewall(t *testing.T) {
	for _, tc := range []struct {
		name        string
		firewallID  string
		description string
		want        string
	}{
		{name: "missing", want: "missing"},
		{name: "ownership mismatch", firewallID: "fw-1", description: "other-deny-public", want: "ownership mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handlerErr := newHandlerErrorRecorder(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/instances":
					_, _ = w.Write([]byte(`{"instances":[{"id":"abc","label":"demo-dev","region":"ewr","plan":"vc2-1c-1gb","os_id":2284,"firewall_group_id":"` + tc.firewallID + `","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:dev"]}],"meta":{"links":{"next":"","prev":""}}}`))
				case "/firewalls/fw-1":
					_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-1","description":"` + tc.description + `"}}`))
				default:
					handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer ts.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
			_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
			handlerErr.check()
			if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), tc.want) {
				t.Fatalf("expected %s firewall failure, got %v", tc.want, diagnostics.Err())
			}
		})
	}
}

func TestListInstancesPagesAndMapsFirewall(t *testing.T) {
	calls := 0
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		if r.URL.Path == "/firewalls/fw-1" {
			_, _ = w.Write([]byte(`{"firewall_group":{"id":"fw-1","description":"demo-dev-deny-public"}}`))
			return
		}
		if r.URL.Path != "/instances" {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"instances":[{"id":"abc","label":"demo-dev","region":"ewr","plan":"vc2-1c-1gb","os_id":2284,"main_ip":"203.0.113.20","power_status":"running","firewall_group_id":"fw-1","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:dev"]}],"meta":{"links":{"next":"cursor-2","prev":""}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"instances":[{"id":"xyz","label":"other","region":"ewr","plan":"vc2-1c-1gb","os_id":2284,"main_ip":"203.0.113.21","power_status":"running","tags":[]}],"meta":{"links":{"next":"","prev":""}}}`))
	}))
	defer ts.Close()

	instances, err := NewWithHTTP("token", ts.URL, ts.Client()).ListInstances(context.Background())
	handlerErr.check()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 || instances[0].FirewallGroupID != "fw-1" {
		t.Fatalf("instances=%+v", instances)
	}

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	calls = 0
	records, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	if err := diagnostics.Err(); err != nil {
		t.Fatal(err)
	}
	namespace, server, ok := ownership.OwnershipFromLabels(records[0].Labels)
	if !ok || namespace != "demo" || server != "dev" {
		t.Fatalf("record=%+v", records[0])
	}
	if records[0].ProviderState["firewall_group_id"] != "fw-1" {
		t.Fatalf("provider state=%+v", records[0].ProviderState)
	}
}
