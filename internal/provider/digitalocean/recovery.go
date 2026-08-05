package digitalocean

import (
	"fmt"
	"strings"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/ownership"
)

func recoverServerRecords(droplets []Droplet, firewalls []Firewall) ([]compute.ServerRecord, error) {
	records := make([]compute.ServerRecord, 0, len(droplets))
	for _, droplet := range droplets {
		record := serverRecordFromLive(droplet)
		if _, _, managed := ownership.OwnershipFromLabels(record.Labels); managed {
			if err := validateRecoveryMetadata(record); err != nil {
				return nil, err
			}
			firewallID, err := uniqueFirewallID(record, firewalls)
			if err != nil {
				return nil, err
			}
			record.ProviderState = map[string]string{"firewall_id": firewallID}
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
		{name: "region", value: record.Location},
		{name: "size", value: record.Size},
		{name: "image", value: record.Image},
	} {
		if field.value == "" || field.name == "id" && field.value == "0" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("provider droplet recovery metadata missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func uniqueFirewallID(record compute.ServerRecord, firewalls []Firewall) (string, error) {
	var matches []string
	for _, firewall := range firewalls {
		if validateLiveFirewallOwnership(record, firewall) == nil {
			matches = append(matches, firewall.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("provider managed firewall missing for %q", record.Name)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("provider managed firewall ambiguous for %q", record.Name)
	}
}
