package digitalocean

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestComputeProviderDeleteCleansFirewallWithoutDropletID(t *testing.T) {
	deletedFirewall := false
	targetTag := firewallTargetTag("prod", "web")
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["` + targetTag + `"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
			deletedFirewall = true
			w.WriteHeader(http.StatusNoContent)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	record := digitalOceanRecord()
	record.ID = ""
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  record,
	})
	if !diagnostics.Passed() || !deletedFirewall {
		t.Fatalf("diagnostics=%v deletedFirewall=%t", diagnostics.Err(), deletedFirewall)
	}
}

func TestComputeProviderDeleteHandlesLegacyFirewallSelectorsSafely(t *testing.T) {
	const legacyFirewall = `{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"],"droplet_ids":[]}}`
	const targetDroplet = `{"id":3164444,"name":"prod-web","tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"]}`
	tests := []struct {
		name               string
		firewall           string
		dropletMissing     bool
		inventory          string
		inventoryFails     bool
		wantPass           bool
		wantDropletDelete  bool
		wantFirewallDelete bool
	}{
		{name: "deleted target with no matches", dropletMissing: true, inventory: `{"droplets":[],"links":{"pages":{}},"meta":{"total":0}}`, wantPass: true, wantFirewallDelete: true},
		{name: "live target is only match", inventory: `{"droplets":[` + targetDroplet + `],"links":{"pages":{}},"meta":{"total":1}}`, wantPass: true, wantDropletDelete: true, wantFirewallDelete: true},
		{name: "unrelated managed droplet matches managed-by selector", inventory: `{"droplets":[` + targetDroplet + `,{"id":987654,"name":"other-api","tags":["managed-by:serverpro","serverpro-namespace:other","serverpro-server:api"]}],"links":{"pages":{}},"meta":{"total":2}}`},
		{name: "unrelated droplet matches namespace selector", inventory: `{"droplets":[` + targetDroplet + `,{"id":987654,"name":"manual","tags":["serverpro-namespace:prod"]}],"links":{"pages":{}},"meta":{"total":2}}`},
		{name: "unrelated droplet matches server selector", inventory: `{"droplets":[` + targetDroplet + `,{"id":987654,"name":"manual","tags":["serverpro-server:web"]}],"links":{"pages":{}},"meta":{"total":2}}`},
		{name: "inventory failure", inventoryFails: true},
		{name: "missing legacy selector", firewall: `{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:prod"],"droplet_ids":[]}}`},
		{name: "extra legacy selector", firewall: `{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web","environment:production"],"droplet_ids":[]}}`},
		{name: "foreign legacy selector", firewall: `{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:other","serverpro-server:web"],"droplet_ids":[]}}`},
		{name: "direct legacy attachment", firewall: `{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro-namespace:prod","serverpro-server:web"],"droplet_ids":[3164444]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dropletDeleted := false
			firewallDeleted := false
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			defer handlerErr.Check()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
					if test.dropletMissing {
						http.NotFound(w, r)
						return
					}
					_, _ = w.Write([]byte(`{"droplet":` + targetDroplet + `}`))
				case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
					firewall := test.firewall
					if firewall == "" {
						firewall = legacyFirewall
					}
					_, _ = w.Write([]byte(firewall))
				case r.Method == http.MethodGet && r.URL.Path == "/droplets":
					if test.inventoryFails {
						http.Error(w, "inventory unavailable", http.StatusBadGateway)
						return
					}
					_, _ = w.Write([]byte(test.inventory))
				case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
					dropletDeleted = true
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
					firewallDeleted = true
					w.WriteHeader(http.StatusNoContent)
				default:
					handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
				}
			}))
			defer srv.Close()

			provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
			diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
				Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
				Record:  digitalOceanRecord(),
			})
			if diagnostics.Passed() != test.wantPass || dropletDeleted != test.wantDropletDelete || firewallDeleted != test.wantFirewallDelete {
				t.Fatalf("diagnostics=%v dropletDeleted=%t firewallDeleted=%t", diagnostics.Err(), dropletDeleted, firewallDeleted)
			}
		})
	}
}

func TestComputeProviderDeleteIgnoresMissingFirewall(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","status":"active","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			http.NotFound(w, r)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if !diagnostics.Passed() {
		t.Fatalf("diagnostics=%v", diagnostics.Err())
	}
}

func TestComputeProviderDeleteContinuesAfterDropletAlreadyDeleted(t *testing.T) {
	deletedFirewall := false
	targetTag := firewallTargetTag("prod", "web")
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["` + targetTag + `"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
			deletedFirewall = true
			w.WriteHeader(http.StatusNoContent)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if !diagnostics.Passed() || !deletedFirewall {
		t.Fatalf("diagnostics=%v deletedFirewall=%t", diagnostics.Err(), deletedFirewall)
	}
}

func TestComputeProviderDeleteRejectsOwnershipMismatch(t *testing.T) {
	deleteCalled := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"other-web","tags":["managed-by:serverpro","serverpro.namespace:other","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			deleteCalled = true
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership mismatch") || deleteCalled {
		t.Fatalf("diagnostics=%v deleteCalled=%t", diagnostics.Err(), deleteCalled)
	}
}

func TestComputeProviderDeleteRejectsMismatchedFirewall(t *testing.T) {
	dropletDeleteCalled := false
	firewallDeleteCalled := false
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			dropletDeleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"other-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
			firewallDeleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "ownership") || dropletDeleteCalled || firewallDeleteCalled {
		t.Fatalf("diagnostics=%v dropletDeleteCalled=%t firewallDeleteCalled=%t", diagnostics.Err(), dropletDeleteCalled, firewallDeleteCalled)
	}
}

func TestComputeProviderDeleteRejectsDirectFirewallDropletAttachments(t *testing.T) {
	dropletDeleteCalled := false
	firewallDeleteCalled := false
	targetTag := firewallTargetTag("prod", "web")
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/droplets/3164444":
			_, _ = w.Write([]byte(`{"droplet":{"id":3164444,"name":"prod-web","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/droplets/3164444":
			dropletDeleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/firewalls/fw-9":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["` + targetTag + `"],"droplet_ids":[987654]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/firewalls/fw-9":
			firewallDeleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Delete(context.Background(), compute.DeleteServerRequest{
		Account: compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Record:  digitalOceanRecord(),
	})
	if diagnostics.Passed() || diagnostics.Err() == nil || !strings.Contains(diagnostics.Err().Error(), "droplet attachment") || dropletDeleteCalled || firewallDeleteCalled {
		t.Fatalf("diagnostics=%v dropletDeleteCalled=%t firewallDeleteCalled=%t", diagnostics.Err(), dropletDeleteCalled, firewallDeleteCalled)
	}
}

func TestRecoverFirewallIDRejectsDirectDropletAttachments(t *testing.T) {
	record := digitalOceanRecord()
	_, err := recoverFirewallID(record, []Firewall{{
		ID:         "fw-9",
		Name:       firewallName(record.Name),
		Tags:       firewallTargetTags(record.Namespace, record.Server),
		DropletIDs: []int64{987654},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateLiveFirewallOwnershipRequiresExactTagTarget(t *testing.T) {
	record := digitalOceanRecord()
	targetTag := firewallTargetTag(record.Namespace, record.Server)
	for _, test := range []struct {
		name       string
		tags       []string
		dropletIDs []int64
		wantErr    bool
	}{
		{name: "exact target", tags: []string{targetTag}},
		{name: "missing target", tags: nil, wantErr: true},
		{name: "broad ownership target", tags: digitalOceanOwnershipTags(record.Namespace, record.Server), wantErr: true},
		{name: "extra target", tags: []string{targetTag, "environment:production"}, wantErr: true},
		{name: "foreign direct droplet", tags: []string{targetTag}, dropletIDs: []int64{987654}, wantErr: true},
		{name: "tracked direct droplet", tags: []string{targetTag}, dropletIDs: []int64{3164444}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLiveFirewallOwnership(record, Firewall{
				Name:       firewallName(record.Name),
				Tags:       test.tags,
				DropletIDs: test.dropletIDs,
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestComputeProviderCreateRedactsBootstrapDataFromDropletErrors(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	authKey := strings.Join([]string{"tskey", "auth", "sensitive"}, "-")
	passwordHash := "$6$rounds=100000$abcdefghijklmnop$" + strings.Repeat("A", 86)
	bootstrapData := "#cloud-config\n" + authKey + "\nhashed_passwd: " + passwordHash + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case handleCreateTag(handlerErr, w, r):
		case r.Method == http.MethodPost && r.URL.Path == "/firewalls":
			_, _ = w.Write([]byte(`{"firewall":{"id":"fw-9","name":"prod-web-deny-public","tags":["managed-by:serverpro","serverpro.namespace:prod","serverpro.server:web"]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/droplets":
			http.Error(w, "echoed user_data "+bootstrapData, http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/droplets" && r.URL.Query().Get("name") == "prod-web":
			_, _ = w.Write([]byte(`{"droplets":[],"links":{"pages":{}},"meta":{"total":0}}`))
		default:
			handlerErr.Record(w, "unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	_, diagnostics := provider.Create(context.Background(), compute.CreateServerRequest{
		Account:       compute.Account{Name: "prod", Provider: "digitalocean", Token: "token"},
		Intent:        digitalOceanIntent(),
		BootstrapData: bootstrapData,
	})
	message := diagnostics.Err().Error()
	if diagnostics.Passed() || strings.Contains(message, authKey) || strings.Contains(message, passwordHash) || strings.Contains(message, bootstrapData) {
		t.Fatalf("bootstrap secrets leaked in diagnostics: %v", diagnostics.Err())
	}
}

func digitalOceanRecord() compute.ServerRecord {
	return compute.ServerRecord{
		Provider:  "digitalocean",
		Account:   "prod",
		ID:        "3164444",
		Name:      "prod-web",
		Namespace: "prod",
		Server:    "web",
		ProviderState: map[string]string{
			"firewall_id": "fw-9",
		},
	}
}
