package vultr

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/ownership"
	"github.com/assagman/serverpro/internal/redact"
)

var bootstrapSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`tskey-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`\$6\$[^\s"']+`),
}

func labelsToTags(labels map[string]string) []string {
	return ownership.ProviderTags(labels)
}

func tagsToLabels(tags []string) map[string]string {
	return ownership.LabelsFromProviderTags(tags)
}

func firewallGroupName(serverName string) string {
	return serverName + "-deny-public"
}

func osIDFromImage(image string) (int64, error) {
	id, err := strconv.ParseInt(image, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("provider os id missing")
	}
	return id, nil
}

func instanceID(record compute.ServerRecord) (string, error) {
	if record.ID == "" {
		return "", fmt.Errorf("provider instance id missing")
	}
	return record.ID, nil
}

func firewallGroupID(record compute.ServerRecord) (string, bool) {
	raw := record.ProviderState["firewall_group_id"]
	return raw, raw != ""
}

func validateMutationRequest(_ compute.Account, record compute.ServerRecord, provider compute.ProviderName) error {
	if record.Provider != provider {
		return fmt.Errorf("provider ownership mismatch: state provider %q provider %q", record.Provider, provider)
	}
	return nil
}

func validateLiveInstanceOwnership(record compute.ServerRecord, inst Instance) error {
	if record.Name != "" && inst.Label != "" && inst.Label != record.Name {
		return fmt.Errorf("provider ownership mismatch: live instance label %q state name %q", inst.Label, record.Name)
	}
	return ownership.ValidateLiveLabels(tagsToLabels(inst.Tags), record.Namespace, record.Server)
}

func validateLiveFirewallGroupOwnership(record compute.ServerRecord, fw FirewallGroup) error {
	if record.Name == "" {
		return fmt.Errorf("provider ownership mismatch: state server name missing")
	}
	expected := firewallGroupName(record.Name)
	if fw.Description != expected {
		return fmt.Errorf("provider ownership mismatch: live firewall group description %q state %q", fw.Description, expected)
	}
	return nil
}

func failure(secret, prefix string, err error, extraSecrets ...string) compute.Diagnostics {
	secrets := append([]string{secret}, extraSecrets...)
	message := redact.New(secrets...).String(fmt.Sprintf("%s: %v", prefix, err))
	return compute.Diagnostics{{Status: compute.Fail, Message: message}}
}

func bootstrapRedactionSecrets(data string) []string {
	secrets := []string{data}
	seen := map[string]bool{data: true}
	encoded := encodeUserData(data)
	if !seen[encoded] {
		secrets = append(secrets, encoded)
		seen[encoded] = true
	}
	for _, pattern := range bootstrapSecretPatterns {
		for _, match := range pattern.FindAllString(data, -1) {
			if !seen[match] {
				secrets = append(secrets, match)
				seen[match] = true
			}
		}
	}
	return secrets
}
