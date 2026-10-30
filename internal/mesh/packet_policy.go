package mesh

import (
	"fmt"
	"slices"
	"strings"
)

const (
	// policyWildcard is the policy-file selector for every source, every
	// destination host, or every protocol and port.
	policyWildcard = "*"
	// policyWildcardHostPort is the first-generation ACL destination for every
	// host on every port.
	policyWildcardHostPort = "*:*"
	// policyActionAccept is the only action first-generation ACLs define.
	policyActionAccept = "accept"
	// policyAllMembers selects the devices of every tailnet member, which for
	// a reachable server is every person's device: as wide as "*" in practice.
	policyAllMembers = "autogroup:member"
	// policyAllIPv4 and policyAllIPv6 are the CIDR spellings of every address.
	policyAllIPv4 = "0.0.0.0/0"
	policyAllIPv6 = "::/0"
	// policyAllPortsFromZero and policyAllPortsFromOne are the range
	// spellings of every port.
	policyAllPortsFromZero = "0-65535"
	policyAllPortsFromOne  = "1-65535"
	// policyPortListSeparator splits an ACL destination's port list.
	policyPortListSeparator = ","
)

// A rule allows all when a selector is any spelling of "everything", not only
// the literal wildcard; a policy that writes 0.0.0.0/0 or 1-65535 opens the
// same ports as one that writes "*".
var (
	allSources = []string{policyWildcard, policyAllMembers, policyAllIPv4, policyAllIPv6}
	allHosts   = []string{policyWildcard, policyAllIPv4, policyAllIPv6}
	allPorts   = []string{policyWildcard, policyAllPortsFromZero, policyAllPortsFromOne}
)

// PacketPolicy is the network-layer part of a tailnet policy file: the
// first-generation acls and the grants that replace them. The sections are
// pointers because an absent section differs from an empty one: Tailscale
// applies its default allow-all policy to a file with neither, while an empty
// acls list denies everything.
type PacketPolicy struct {
	ACLs   *[]ACLRule `json:"acls"`
	Grants *[]Grant   `json:"grants"`
}

// ACLRule is one first-generation acls entry. Users and Ports are the legacy
// spellings of src and dst that old policy files still carry; JSON field
// matching is case-insensitive, so "Users" and "Ports" land here too.
type ACLRule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
	Users  []string `json:"users"`
	Ports  []string `json:"ports"`
}

// Grant is one grants entry; only the network capability (ip) matters for
// packet filtering, so application capabilities are not modeled.
type Grant struct {
	Src []string `json:"src"`
	Dst []string `json:"dst"`
	IP  []string `json:"ip"`
}

// AllowAllRules names every rule that lets every tailnet source reach every
// device on every port, or the implicit default when the file has neither
// section. Doctor warns on any of them: such a rule makes UFW the only
// barrier between any tailnet device and every port a host listens on.
func (p PacketPolicy) AllowAllRules() []string {
	if p.ACLs == nil && p.Grants == nil {
		return []string{"no acls or grants section: Tailscale applies its default allow-all policy"}
	}
	var rules []string
	if p.ACLs != nil {
		for i, rule := range *p.ACLs {
			if source, destination, ok := rule.allowsAll(); ok {
				rules = append(rules, fmt.Sprintf("acls[%d] accepts %s to %s", i, source, destination))
			}
		}
	}
	if p.Grants != nil {
		for i, grant := range *p.Grants {
			if source, destination, ports, ok := grant.allowsAll(); ok {
				rules = append(rules, fmt.Sprintf("grants[%d] grants %s to %s on ip %s", i, source, destination, ports))
			}
		}
	}
	return rules
}

// allowsAll returns the matching source and destination, so the finding
// names the spelling the policy file uses.
func (r ACLRule) allowsAll() (string, string, bool) {
	if !strings.EqualFold(r.Action, policyActionAccept) {
		return "", "", false
	}
	source, found := firstOf(append(append([]string(nil), r.Src...), r.Users...), allSources)
	if !found {
		return "", "", false
	}
	for _, destination := range append(append([]string(nil), r.Dst...), r.Ports...) {
		if aclDestinationAllowsAll(destination) {
			return source, destination, true
		}
	}
	return "", "", false
}

func (g Grant) allowsAll() (string, string, string, bool) {
	source, sourceFound := firstOf(g.Src, allSources)
	destination, destinationFound := firstOf(g.Dst, allHosts)
	ports, portsFound := firstOf(g.IP, allPorts)
	return source, destination, ports, sourceFound && destinationFound && portsFound
}

// aclDestinationAllowsAll reads a first-generation <host>:<ports>
// destination. The ports follow the last colon, so an IPv6 host such as
// ::/0 or [::/0] keeps its own colons, and one all-ports entry in a port
// list opens every port.
func aclDestinationAllowsAll(destination string) bool {
	if destination == policyWildcardHostPort {
		return true
	}
	separator := strings.LastIndex(destination, ":")
	if separator < 0 {
		return false
	}
	host := strings.TrimSuffix(strings.TrimPrefix(destination[:separator], "["), "]")
	if !slices.Contains(allHosts, host) {
		return false
	}
	_, found := firstOf(strings.Split(destination[separator+1:], policyPortListSeparator), allPorts)
	return found
}

func firstOf(selectors, everything []string) (string, bool) {
	for _, selector := range selectors {
		if slices.Contains(everything, strings.TrimSpace(selector)) {
			return selector, true
		}
	}
	return "", false
}
