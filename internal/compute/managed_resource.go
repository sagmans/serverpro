package compute

import "fmt"

var legacyAccessPolicyKeys = [...]string{"access_policy_id", "firewall_id", "firewall_group_id"}

// ManagedResourceKind identifies provider-neutral resources owned with a server.
type ManagedResourceKind string

const ManagedResourceAccessPolicy ManagedResourceKind = "access_policy"

// ManagedResourceRef records an external resource without exposing adapter keys.
type ManagedResourceRef struct {
	Kind ManagedResourceKind `json:"kind"`
	ID   string              `json:"id"`
}

// LegacyManagedResources migrates historical adapter-map IDs at read boundaries.
func LegacyManagedResources(providerState map[string]string) []ManagedResourceRef {
	for _, key := range legacyAccessPolicyKeys {
		if id := providerState[key]; id != "" {
			return []ManagedResourceRef{{Kind: ManagedResourceAccessPolicy, ID: id}}
		}
	}
	return nil
}

// CanonicalManagedResources resolves one access-policy identity and removes its
// legacy adapter keys while preserving unrelated provider state.
func CanonicalManagedResources(resources []ManagedResourceRef, providerState map[string]string) ([]ManagedResourceRef, map[string]string, error) {
	canonical := make([]ManagedResourceRef, 0, len(resources)+1)
	accessPolicyID := ""
	setAccessPolicyID := func(id string) error {
		if id == "" {
			return nil
		}
		if accessPolicyID != "" && accessPolicyID != id {
			return fmt.Errorf("conflicting managed access policy ids %q and %q", accessPolicyID, id)
		}
		accessPolicyID = id
		return nil
	}
	for _, resource := range resources {
		if resource.Kind != ManagedResourceAccessPolicy {
			canonical = append(canonical, resource)
			continue
		}
		if err := setAccessPolicyID(resource.ID); err != nil {
			return nil, nil, err
		}
	}
	cleanedState := make(map[string]string, len(providerState))
	for key, value := range providerState {
		legacy := false
		for _, legacyKey := range legacyAccessPolicyKeys {
			if key == legacyKey {
				legacy = true
				if err := setAccessPolicyID(value); err != nil {
					return nil, nil, err
				}
				break
			}
		}
		if !legacy {
			cleanedState[key] = value
		}
	}
	if accessPolicyID != "" {
		canonical = append(canonical, ManagedResourceRef{Kind: ManagedResourceAccessPolicy, ID: accessPolicyID})
	}
	if len(cleanedState) == 0 {
		cleanedState = nil
	}
	return canonical, cleanedState, nil
}

// ManagedResourceID returns the first non-empty resource ID of the requested kind.
func ManagedResourceID(resources []ManagedResourceRef, kind ManagedResourceKind) (string, bool) {
	for _, resource := range resources {
		if resource.Kind == kind && resource.ID != "" {
			return resource.ID, true
		}
	}
	return "", false
}
