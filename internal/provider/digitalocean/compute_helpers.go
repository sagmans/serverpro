package digitalocean

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/redact"
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

func firewallName(serverName string) string {
	return serverName + "-deny-public"
}

const (
	tailscaleWireGuardPort  = "41641"
	legacyTailscaleSTUNPort = "3478"
)

var publicIPRanges = []string{"0.0.0.0/0", "::/0"}

func tailscaleInboundRules() []Rule {
	sources := &RuleTargets{Addresses: slices.Clone(publicIPRanges)}
	return []Rule{{Protocol: "udp", Ports: tailscaleWireGuardPort, Sources: sources}}
}

func legacyTailscaleSTUNRules(rules []Rule) []Rule {
	var legacy []Rule
	for _, rule := range rules {
		if rule.Protocol != "udp" || rule.Ports != legacyTailscaleSTUNPort || rule.Sources == nil {
			continue
		}
		addresses := slices.Clone(rule.Sources.Addresses)
		slices.Sort(addresses)
		expected := slices.Clone(publicIPRanges)
		slices.Sort(expected)
		if slices.Equal(addresses, expected) && len(rule.Sources.DropletIDs) == 0 && len(rule.Sources.LoadBalancerUIDs) == 0 && len(rule.Sources.KubernetesIDs) == 0 && len(rule.Sources.Tags) == 0 {
			legacy = append(legacy, rule)
		}
	}
	return legacy
}

func allowAllOutboundRules() []Rule {
	destinations := &RuleTargets{Addresses: []string{"0.0.0.0/0", "::/0"}}
	return []Rule{
		{Protocol: "tcp", Ports: "0", Destinations: destinations},
		{Protocol: "udp", Ports: "0", Destinations: destinations},
		{Protocol: "icmp", Ports: "0", Destinations: destinations},
	}
}

func dropletID(record compute.ServerRecord) (int64, error) {
	id, err := strconv.ParseInt(record.ID, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("provider droplet id missing")
	}
	return id, nil
}

func firewallID(record compute.ServerRecord) (string, bool) {
	raw := record.ProviderState["firewall_id"]
	return raw, raw != ""
}

func validateMutationRequest(_ compute.Account, record compute.ServerRecord, provider compute.ProviderName) error {
	if record.Provider != provider {
		return fmt.Errorf("provider ownership mismatch: state provider %q provider %q", record.Provider, provider)
	}
	return nil
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
	return ownership.ValidateLiveLabels(tagsToLabels(firewall.Tags), record.Namespace, record.Server)
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
	size := droplet.SizeSlug
	if size == "" {
		size = droplet.Size.Slug
	}
	image := droplet.Image.Slug
	return compute.ServerRecord{
		Provider:   compute.ProviderName("digitalocean"),
		Namespace:  namespace,
		Server:     server,
		ID:         strconv.FormatInt(droplet.ID, 10),
		Name:       droplet.Name,
		Location:   droplet.Region.Slug,
		Size:       size,
		Image:      image,
		PublicIPv4: publicIPv4(droplet),
		Labels:     labels,
	}
}

func failure(secret, prefix string, err error, extraSecrets ...string) compute.Diagnostics {
	secrets := append([]string{secret}, extraSecrets...)
	message := redact.New(secrets...).String(fmt.Sprintf("%s: %v", prefix, err))
	return compute.Diagnostics{{Status: compute.Fail, Message: message}}
}

func bootstrapRedactionSecrets(data string) []string {
	secrets := []string{data}
	seen := map[string]bool{data: true}
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
