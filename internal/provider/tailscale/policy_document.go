package tailscale

import (
	"encoding/json"
	"sort"
)

type policyDocument map[string]json.RawMessage

const serverproPolicyOwner = "autogroup:admin"

func (d policyDocument) ensureTagOwners(tags []string) ([]string, error) {
	owners, err := d.tagOwners()
	if err != nil {
		return nil, err
	}
	added := []string{}
	for _, tag := range tags {
		if _, ok := owners[tag]; ok {
			continue
		}
		owners[tag] = []string{serverproPolicyOwner}
		added = append(added, tag)
	}
	if len(added) == 0 {
		return nil, nil
	}
	sort.Strings(added)
	return added, d.setTagOwners(owners)
}

func (d policyDocument) removeTagOwners(tags []string) (bool, error) {
	owners, err := d.tagOwners()
	if err != nil {
		return false, err
	}
	changed := false
	for _, tag := range tags {
		if _, ok := owners[tag]; !ok {
			continue
		}
		delete(owners, tag)
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, d.setTagOwners(owners)
}

func (d policyDocument) tagOwners() (map[string][]string, error) {
	owners := map[string][]string{}
	raw, ok := d["tagOwners"]
	if !ok || len(raw) == 0 {
		return owners, nil
	}
	return owners, json.Unmarshal(raw, &owners)
}

func (d policyDocument) setTagOwners(owners map[string][]string) error {
	if len(owners) == 0 {
		delete(d, "tagOwners")
		return nil
	}
	b, err := json.Marshal(owners)
	if err != nil {
		return err
	}
	d["tagOwners"] = b
	return nil
}
