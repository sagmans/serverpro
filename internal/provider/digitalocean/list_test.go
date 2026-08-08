package digitalocean

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

func TestListDropletsMapsTags(t *testing.T) {
	targetTag := firewallTargetTag("demo", "web")
	firewallPages := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		switch r.URL.Path {
		case "/droplets":
			_, _ = w.Write([]byte(`{"droplets":[{"id":99,"name":"demo-web","status":"active","networks":{"v4":[{"ip_address":"203.0.113.30","type":"public"}]},"tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
		case "/firewalls":
			firewallPages++
			if firewallPages == 1 {
				_, _ = w.Write([]byte(`{"firewalls":[{"id":"fw-8","name":"other-deny-public","tags":[]}],"links":{"pages":{"next":"https://api.digitalocean.com/v2/firewalls?page=2"}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"firewalls":[{"id":"fw-9","name":"demo-web-deny-public","tags":["` + targetTag + `"]}],"links":{}}`))
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	droplets, err := NewWithHTTP("token", ts.URL, ts.Client()).ListDroplets(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(droplets) != 1 || droplets[0].ID != 99 {
		t.Fatalf("droplets=%+v", droplets)
	}

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	records, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	if err := diagnostics.Err(); err != nil {
		t.Fatal(err)
	}
	namespace, server, ok := ownership.OwnershipFromLabels(records[0].Labels)
	policyID, found := compute.ManagedResourceID(records[0].ManagedResources, compute.ManagedResourceAccessPolicy)
	if !ok || namespace != "demo" || server != "web" || records[0].PublicIPv4 != "203.0.113.30" || !found || policyID != "fw-9" || firewallPages != 2 {
		t.Fatalf("record=%+v ns=%s server=%s ok=%t firewallPages=%d", records[0], namespace, server, ok, firewallPages)
	}
}

func TestListRecoversBoundedLegacyAccessPolicy(t *testing.T) {
	const targetDroplet = `{"id":99,"name":"demo-web","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}`
	const legacyFirewall = `{"id":"fw-9","name":"demo-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}`
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/droplets":
			_, _ = w.Write([]byte(`{"droplets":[` + targetDroplet + `],"links":{}}`))
		case "/firewalls":
			_, _ = w.Write([]byte(`{"firewalls":[` + legacyFirewall + `],"links":{}}`))
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	records, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	handlerErr.Check()
	if err := diagnostics.Err(); err != nil {
		t.Fatal(err)
	}
	policyID, found := compute.ManagedResourceID(records[0].ManagedResources, compute.ManagedResourceAccessPolicy)
	if !found || policyID != "fw-9" {
		t.Fatalf("legacy access policy not recovered: %+v", records[0])
	}
}

func TestListRejectsLegacyAccessPolicyWithUnrelatedMatch(t *testing.T) {
	const targetDroplet = `{"id":99,"name":"demo-web","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}`
	const unrelatedDroplet = `{"id":100,"name":"other-api","tags":["managed-by:serverpro","serverpro-namespace:other","serverpro-server:api"]}`
	const legacyFirewall = `{"id":"fw-9","name":"demo-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}`
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/droplets":
			_, _ = w.Write([]byte(`{"droplets":[` + targetDroplet + `,` + unrelatedDroplet + `],"links":{}}`))
		case "/firewalls":
			_, _ = w.Write([]byte(`{"firewalls":[` + legacyFirewall + `],"links":{}}`))
		default:
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	handlerErr.Check()
	if err := diagnostics.Err(); err == nil || !strings.Contains(err.Error(), "unrelated live droplet") {
		t.Fatalf("diagnostics=%v", err)
	}
}

func TestListRejectsIncompleteManagedAccessPolicyRecovery(t *testing.T) {
	targetTag := firewallTargetTag("demo", "web")
	for _, test := range []struct {
		name      string
		firewalls string
		want      string
	}{
		{name: "missing", firewalls: `{"firewalls":[],"links":{}}`, want: "not found"},
		{name: "ambiguous", firewalls: `{"firewalls":[{"id":"fw-9","name":"demo-web-deny-public","tags":["` + targetTag + `"]},{"id":"fw-10","name":"demo-web-deny-public","tags":["` + targetTag + `"]}],"links":{}}`, want: "ambiguous"},
		{name: "missing id", firewalls: `{"firewalls":[{"name":"demo-web-deny-public","tags":["` + targetTag + `"]}],"links":{}}`, want: "id missing"},
		{name: "foreign direct droplet", firewalls: `{"firewalls":[{"id":"fw-9","name":"demo-web-deny-public","tags":["` + targetTag + `"],"droplet_ids":[987654]}],"links":{}}`, want: "not found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/droplets":
					_, _ = w.Write([]byte(`{"droplets":[{"id":99,"name":"demo-web","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
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
		if r.URL.Path != "/droplets" {
			handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"droplets":[{"id":99,"name":"manual","tags":[]}],"links":{}}`))
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
		if r.URL.Path == "/droplets" {
			_, _ = w.Write([]byte(`{"droplets":[{"id":99,"name":"demo-web","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
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
