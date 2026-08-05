package state

import (
	"os"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
)

func TestLoadRejectsConflictingTypedAndLegacyManagedResources(t *testing.T) {
	path := t.TempDir() + "/state.json"
	body := `{"namespace":"prod","server":"web","compute":{"managed_resources":[{"kind":"access_policy","id":"typed-1"}],"provider_state":{"firewall_id":"legacy-2"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "conflicting managed access policy ids") {
		t.Fatalf("error = %v", err)
	}
}

func TestSaveWritesOnlyCanonicalManagedResourceIdentity(t *testing.T) {
	path := t.TempDir() + "/state.json"
	st := State{
		Namespace: "prod",
		Server:    "web",
		Compute: ComputeState{
			ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "policy-1"}},
			ProviderState:    map[string]string{"firewall_id": "policy-1", "opaque": "kept"},
		},
	}
	if err := Save(path, st); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "firewall_id") || !strings.Contains(text, `"opaque": "kept"`) || !strings.Contains(text, `"managed_resources"`) {
		t.Fatalf("canonical state = %s", body)
	}
}

func TestLoadMigratesLegacyManagedResourceKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		id   string
	}{
		{name: "hetzner", key: "access_policy_id", id: "1"},
		{name: "digitalocean", key: "firewall_id", id: "fw-1"},
		{name: "vultr", key: "firewall_group_id", id: "group-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/state.json"
			body := `{"namespace":"prod","server":"web","compute":{"provider_state":{"` + tc.key + `":"` + tc.id + `"}}}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			st, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got, ok := compute.ManagedResourceID(st.Compute.ManagedResources, compute.ManagedResourceAccessPolicy); !ok || got != tc.id {
				t.Fatalf("resources=%+v", st.Compute.ManagedResources)
			}
		})
	}
}
