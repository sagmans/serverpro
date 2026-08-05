package hetzner

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/ownership"
)

func recoverServerRecords(servers []Server, firewalls []Firewall) ([]compute.ServerRecord, error) {
	records := make([]compute.ServerRecord, 0, len(servers))
	for _, server := range servers {
		record := serverRecordFromLive(server)
		if _, _, managed := ownership.OwnershipFromLabels(record.Labels); managed {
			if err := validateRecoveryMetadata(record); err != nil {
				return nil, err
			}
			accessPolicyID, err := uniqueAccessPolicyID(record, firewalls)
			if err != nil {
				return nil, err
			}
			record.ProviderState = map[string]string{"access_policy_id": strconv.FormatInt(accessPolicyID, 10)}
		}
		records = append(records, record)
	}
	return records, nil
}

func validateRecoveryMetadata(record compute.ServerRecord) error {
	var missing []string
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "id", value: record.ID},
		{name: "name", value: record.Name},
		{name: "location", value: record.Location},
		{name: "size", value: record.Size},
		{name: "image", value: record.Image},
	} {
		if field.value == "" || field.name == "id" && field.value == "0" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("provider server recovery metadata missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func uniqueAccessPolicyID(record compute.ServerRecord, firewalls []Firewall) (int64, error) {
	var matches []int64
	for _, firewall := range firewalls {
		if validateLiveAccessPolicyOwnership(record, firewall) == nil {
			matches = append(matches, firewall.ID)
		}
	}
	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("provider managed access policy missing for %q", record.Name)
	case 1:
		return matches[0], nil
	default:
		return 0, fmt.Errorf("provider managed access policy ambiguous for %q", record.Name)
	}
}
