package digitalocean

import (
	"fmt"
	"strconv"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/provider/providerutil"
)

const firewallTargetLabelKey = "serverpro-firewall-target"

func labelsToTags(labels map[string]string) []string {
	return ownership.ProviderTags(labels)
}

func tagsToLabels(tags []string) map[string]string {
	return ownership.LabelsFromProviderTags(tags)
}

func firewallTargetTag(namespace, server string) string {
	return firewallTargetTags(namespace, server)[0]
}

func firewallTargetTags(namespace, server string) []string {
	return ownership.ProviderTags(map[string]string{firewallTargetLabelKey: namespace + "/" + server})
}

func firewallTargetsServer(firewall Firewall, namespace, server string) bool {
	return len(firewall.Tags) == 1 &&
		firewall.Tags[0] == firewallTargetTag(namespace, server) &&
		len(firewall.DropletIDs) == 0
}

func firewallName(serverName string) string {
	return serverName + "-deny-public"
}

func tailscaleInboundRules() []Rule {
	sources := &RuleTargets{Addresses: []string{"0.0.0.0/0", "::/0"}}
	return []Rule{
		{Protocol: "udp", Ports: "41641", Sources: sources},
		{Protocol: "udp", Ports: "3478", Sources: sources},
	}
}

func allowAllOutboundRules() []Rule {
	destinations := &RuleTargets{Addresses: []string{"0.0.0.0/0", "::/0"}}
	return []Rule{
		{Protocol: "tcp", Ports: "0", Destinations: destinations},
		{Protocol: "udp", Ports: "0", Destinations: destinations},
		{Protocol: "icmp", Ports: "0", Destinations: destinations},
	}
}

func firewallRulesMatch(live, expected []Rule) bool {
	if len(live) != len(expected) {
		return false
	}
	matched := make([]bool, len(live))
	for _, expectedRule := range expected {
		found := false
		for index, liveRule := range live {
			if !matched[index] && firewallRuleMatches(liveRule, expectedRule) {
				matched[index] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func firewallRuleMatches(live, expected Rule) bool {
	return live.Protocol == expected.Protocol &&
		live.Ports == expected.Ports &&
		firewallRuleTargetsMatch(live.Sources, expected.Sources) &&
		firewallRuleTargetsMatch(live.Destinations, expected.Destinations)
}

func firewallRuleTargetsMatch(live, expected *RuleTargets) bool {
	if expected == nil {
		return live == nil || live.empty()
	}
	return live != nil &&
		multisetsMatch(live.Addresses, expected.Addresses) &&
		multisetsMatch(live.Tags, expected.Tags) &&
		multisetsMatch(live.DropletIDs, expected.DropletIDs) &&
		multisetsMatch(live.LoadBalancerUIDs, expected.LoadBalancerUIDs) &&
		multisetsMatch(live.KubernetesIDs, expected.KubernetesIDs)
}

func (targets RuleTargets) empty() bool {
	return len(targets.Addresses) == 0 &&
		len(targets.Tags) == 0 &&
		len(targets.DropletIDs) == 0 &&
		len(targets.LoadBalancerUIDs) == 0 &&
		len(targets.KubernetesIDs) == 0
}

func multisetsMatch[T comparable](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[T]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func dropletID(record compute.ServerRecord) (int64, error) {
	id, err := strconv.ParseInt(record.ID, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("provider droplet id missing")
	}
	return id, nil
}

func recoverFirewallID(record compute.ServerRecord, firewalls []Firewall) (string, error) {
	expectedName := firewallName(record.Name)
	var matches []Firewall
	for _, firewall := range firewalls {
		if firewall.Name == expectedName && firewallTargetsServer(firewall, record.Namespace, record.Server) {
			matches = append(matches, firewall)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("provider access policy %q not found", expectedName)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("provider access policy %q is ambiguous", expectedName)
	}
	if matches[0].ID == "" {
		return "", fmt.Errorf("provider access policy %q id missing", expectedName)
	}
	return matches[0].ID, nil
}

func firewallID(record compute.ServerRecord) (string, bool) {
	raw, ok := compute.ManagedResourceID(record.ManagedResources, compute.ManagedResourceAccessPolicy)
	if ok {
		return raw, true
	}
	raw = record.ProviderState["firewall_id"]
	return raw, raw != ""
}

func validateMutationRequest(record compute.ServerRecord, provider compute.ProviderName) error {
	return providerutil.ValidateMutationProvider(record.Provider, provider)
}

func validateLiveDropletOwnership(record compute.ServerRecord, droplet Droplet) error {
	if record.Name != "" && droplet.Name != "" && droplet.Name != record.Name {
		return fmt.Errorf("provider ownership mismatch: live droplet name %q state name %q", droplet.Name, record.Name)
	}
	return ownership.ValidateLiveLabels(tagsToLabels(droplet.Tags), record.Namespace, record.Server)
}

func validateLiveFirewallOwnership(record compute.ServerRecord, firewall Firewall) error {
	if record.Name == "" {
		return fmt.Errorf("provider ownership mismatch: state server name missing")
	}
	expected := firewallName(record.Name)
	if firewall.Name != expected {
		return fmt.Errorf("provider ownership mismatch: live firewall name %q state %q", firewall.Name, expected)
	}
	if len(firewall.DropletIDs) != 0 {
		return fmt.Errorf("provider ownership mismatch: live firewall has direct droplet attachment")
	}
	if len(firewall.Tags) != 1 || firewall.Tags[0] != firewallTargetTag(record.Namespace, record.Server) {
		return fmt.Errorf("provider ownership mismatch: live firewall target tags do not match state")
	}
	return nil
}

func publicIPv4(droplet Droplet) string {
	for _, network := range droplet.Networks.V4 {
		if network.Type == "public" {
			return network.IPAddress
		}
	}
	return ""
}

func serverRecordFromLive(droplet Droplet) compute.ServerRecord {
	labels := tagsToLabels(droplet.Tags)
	namespace, server, _ := ownership.OwnershipFromLabels(labels)
	return compute.ServerRecord{
		Provider:   compute.ProviderName("digitalocean"),
		Namespace:  namespace,
		Server:     server,
		ID:         strconv.FormatInt(droplet.ID, 10),
		Name:       droplet.Name,
		PublicIPv4: publicIPv4(droplet),
		Labels:     labels,
	}
}

func failure(secret, prefix string, err error, extraSecrets ...string) compute.Diagnostics {
	return providerutil.Failure(secret, prefix, err, extraSecrets...)
}

func bootstrapRedactionSecrets(data string) []string {
	return providerutil.BootstrapSecrets(data)
}
