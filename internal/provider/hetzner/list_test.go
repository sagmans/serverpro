package hetzner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/ownership"
)

func TestListServersPagesAndMapsOwnership(t *testing.T) {
	page := 0
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
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
			_, _ = w.Write([]byte(`{"firewalls":[{"id":9,"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}],"meta":{"pagination":{"next_page":null}}}`))
		default:
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	servers, err := NewWithHTTP("token", ts.URL, ts.Client()).ListServers(context.Background())
	handlerErr.check()
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
	handlerErr.check()
	if err := diagnostics.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%+v", records)
	}
	namespace, server, ok := ownership.OwnershipFromLabels(records[0].Labels)
	if !ok || namespace != "demo" || server != "web" || records[0].PublicIPv4 != "203.0.113.10" {
		t.Fatalf("record0=%+v ns=%s server=%s ok=%t", records[0], namespace, server, ok)
	}
	if records[0].ProviderState["access_policy_id"] != "9" {
		t.Fatalf("recovery access policy missing: %+v", records[0])
	}
}

func TestValidateRecoveryMetadataRejectsZeroServerID(t *testing.T) {
	record := compute.ServerRecord{ID: "0", Name: "demo-web", Location: "fsn1", Size: "cx23", Image: "ubuntu-24.04"}
	if err := validateRecoveryMetadata(record); err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("expected missing id failure, got %v", err)
	}
}

func TestComputeProviderListRejectsMissingManagedRecoveryMetadata(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/servers":
			_, _ = w.Write([]byte(`{"servers":[{"id":1,"name":"demo-web","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"},"server_type":{"name":"cx23"},"image":{"name":"ubuntu-24.04"}}],"meta":{"pagination":{"next_page":null}}}`))
		case "/firewalls":
			_, _ = w.Write([]byte(`{"firewalls":[{"id":9,"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}],"meta":{"pagination":{"next_page":null}}}`))
		default:
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	handlerErr.check()
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "metadata missing") {
		t.Fatalf("expected missing location metadata failure, got %v", diagnostics.Err())
	}
}

func TestComputeProviderListRequiresUniqueManagedAccessPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		firewalls string
		want      string
	}{
		{name: "missing", firewalls: `[]`, want: "missing"},
		{name: "ambiguous", firewalls: `[{"id":9,"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}},{"id":10,"name":"demo-web-deny-public","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"}}]`, want: "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handlerErr := newHandlerErrorRecorder(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/servers":
					_, _ = w.Write([]byte(`{"servers":[{"id":1,"name":"demo-web","labels":{"managed-by":"serverpro","serverpro-namespace":"demo","serverpro-server":"web"},"server_type":{"name":"cx23"},"image":{"name":"ubuntu-24.04"},"location":{"name":"fsn1"}}],"meta":{"pagination":{"next_page":null}}}`))
				case "/firewalls":
					_, _ = w.Write([]byte(`{"firewalls":` + tc.firewalls + `,"meta":{"pagination":{"next_page":null}}}`))
				default:
					handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer ts.Close()
			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
			_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
			handlerErr.check()
			if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), tc.want) {
				t.Fatalf("expected %s access policy failure, got %v", tc.want, diagnostics.Err())
			}
		})
	}
}
