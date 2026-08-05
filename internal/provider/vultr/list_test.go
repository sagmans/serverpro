package vultr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestListInstancesPagesAndMapsFirewall(t *testing.T) {
	calls := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/instances" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"instances":[{"id":"abc","label":"example-dev","region":"ewr","plan":"vc2-1c-1gb","os_id":2284,"main_ip":"203.0.113.20","power_status":"running","firewall_group_id":"fw-1","tags":["managed-by:serverpro","serverpro-namespace:example","serverpro-server:dev"]}],"meta":{"links":{"next":"cursor-2","prev":""}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"instances":[{"id":"xyz","label":"other","region":"ewr","plan":"vc2-1c-1gb","os_id":2284,"main_ip":"203.0.113.21","power_status":"running","tags":[]}],"meta":{"links":{"next":"","prev":""}}}`))
	}))
	defer ts.Close()

	instances, err := NewWithHTTP("token", ts.URL, ts.Client()).ListInstances(context.Background())
	handlerErr.Check()
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
	if !ok || namespace != "example" || server != "dev" {
		t.Fatalf("record=%+v", records[0])
	}
	policyID, found := compute.ManagedResourceID(records[0].ManagedResources, compute.ManagedResourceAccessPolicy)
	if !found || policyID != "fw-1" || len(records[0].ProviderState) != 0 {
		t.Fatalf("managed resources=%+v provider state=%+v", records[0].ManagedResources, records[0].ProviderState)
	}
}
