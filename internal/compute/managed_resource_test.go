package compute

import "testing"

func TestLegacyManagedResourcesMigratesKnownKeys(t *testing.T) {
	for _, key := range []string{"access_policy_id", "firewall_id", "firewall_group_id"} {
		refs := LegacyManagedResources(map[string]string{key: "policy-1"})
		if id, ok := ManagedResourceID(refs, ManagedResourceAccessPolicy); !ok || id != "policy-1" {
			t.Fatalf("key=%s refs=%+v", key, refs)
		}
	}
	if refs := LegacyManagedResources(map[string]string{"opaque": "value"}); refs != nil {
		t.Fatalf("unexpected refs=%+v", refs)
	}
}

func TestCanonicalManagedResourcesMigratesMatchingLegacyIdentity(t *testing.T) {
	refs, providerState, err := CanonicalManagedResources(
		[]ManagedResourceRef{{Kind: ManagedResourceAccessPolicy, ID: "policy-1"}},
		map[string]string{"firewall_id": "policy-1", "opaque": "kept"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := ManagedResourceID(refs, ManagedResourceAccessPolicy); !ok || id != "policy-1" {
		t.Fatalf("refs=%+v", refs)
	}
	if len(providerState) != 1 || providerState["opaque"] != "kept" {
		t.Fatalf("provider state=%+v", providerState)
	}
}

func TestCanonicalManagedResourcesRejectsConflictingLegacyIdentities(t *testing.T) {
	_, _, err := CanonicalManagedResources(nil, map[string]string{
		"access_policy_id": "policy-1",
		"firewall_id":      "policy-2",
	})
	if err == nil {
		t.Fatal("expected conflicting identity error")
	}
}

func TestManagedResourceIDFindsTypedResource(t *testing.T) {
	refs := []ManagedResourceRef{{Kind: ManagedResourceAccessPolicy, ID: "fw-1"}}
	if got, ok := ManagedResourceID(refs, ManagedResourceAccessPolicy); !ok || got != "fw-1" {
		t.Fatalf("id=%q ok=%t", got, ok)
	}
	if _, ok := ManagedResourceID(refs, ManagedResourceKind("other")); ok {
		t.Fatal("unexpected resource match")
	}
}
