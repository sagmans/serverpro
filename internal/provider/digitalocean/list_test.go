package digitalocean

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
)

func TestListDropletsMapsTags(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
			return
		}
		switch r.URL.Path {
		case "/droplets":
			_, _ = w.Write([]byte(`{"droplets":[{"id":99,"name":"demo-web","status":"active","region":{"slug":"nyc3"},"size_slug":"s-1vcpu-1gb","size":{"slug":"s-1vcpu-1gb"},"image":{"id":2284,"slug":"ubuntu-24-04-x64"},"networks":{"v4":[{"ip_address":"203.0.113.30","type":"public"}]},"tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
		case "/firewalls":
			_, _ = w.Write([]byte(`{"firewalls":[{"id":"fw-9","name":"demo-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
		default:
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	droplets, err := NewWithHTTP("token", ts.URL, ts.Client()).ListDroplets(context.Background())
	handlerErr.check()
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
	if !ok || namespace != "demo" || server != "web" || records[0].PublicIPv4 != "203.0.113.30" {
		t.Fatalf("record=%+v ns=%s server=%s ok=%t", records[0], namespace, server, ok)
	}
	if records[0].Location != "nyc3" || records[0].Size != "s-1vcpu-1gb" || records[0].Image != "ubuntu-24-04-x64" || records[0].ProviderState["firewall_id"] != "fw-9" {
		t.Fatalf("recovery metadata missing: %+v", records[0])
	}
}

func TestValidateRecoveryMetadataRejectsZeroDropletID(t *testing.T) {
	record := compute.ServerRecord{ID: "0", Name: "demo-web", Location: "nyc3", Size: "s-1vcpu-1gb", Image: "ubuntu-24-04-x64"}
	if err := validateRecoveryMetadata(record); err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("expected missing id failure, got %v", err)
	}
}

func TestComputeProviderListRejectsMissingManagedRecoveryMetadata(t *testing.T) {
	handlerErr := newHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/droplets":
			_, _ = w.Write([]byte(`{"droplets":[{"id":99,"name":"demo-web","region":{"slug":"nyc3"},"size_slug":"s-1vcpu-1gb","image":{"id":2284},"tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
		case "/firewalls":
			_, _ = w.Write([]byte(`{"firewalls":[{"id":"fw-9","name":"demo-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
		default:
			handlerErr.record(w, "unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, ts.URL, ts.Client()) })
	_, diagnostics := provider.List(context.Background(), compute.ListServersQuery{Account: compute.Account{Token: "token"}})
	handlerErr.check()
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "metadata missing") {
		t.Fatalf("expected missing image metadata failure, got %v", diagnostics.Err())
	}
}

func TestComputeProviderListRequiresUniqueManagedFirewall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		firewalls string
		want      string
	}{
		{name: "missing", firewalls: `[]`, want: "missing"},
		{name: "ambiguous", firewalls: `[{"id":"fw-1","name":"demo-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]},{"id":"fw-2","name":"demo-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}]`, want: "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handlerErr := newHandlerErrorRecorder(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/droplets":
					_, _ = w.Write([]byte(`{"droplets":[{"id":99,"name":"demo-web","region":{"slug":"nyc3"},"size_slug":"s-1vcpu-1gb","image":{"slug":"ubuntu-24-04-x64"},"tags":["managed-by:serverpro","serverpro-namespace:demo","serverpro-server:web"]}],"links":{}}`))
				case "/firewalls":
					_, _ = w.Write([]byte(`{"firewalls":` + tc.firewalls + `,"links":{}}`))
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
