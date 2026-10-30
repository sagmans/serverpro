package mesh

import (
	"fmt"
	"net/netip"
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
	// policyAllSourcesAnywhere selects every device, even ones outside the
	// tailnet, so as a source it is at least as wide as "*". Tailscale keeps
	// it only for backward compatibility and allows it only in src.
	policyAllSourcesAnywhere = "autogroup:danger-all"
	// policyIPSetPrefix starts a reference to a named IP set, whose add and
	// remove lines run in order and may nest other sets. Doctor does not
	// evaluate them, so it fails closed: a set counts as every address, and
	// the finding says so, so the operator checks the set by hand.
	policyIPSetPrefix = "ipset:"
	// policyIPSetUnresolved is what a finding shows in place of the addresses
	// of an IP set, since doctor never reads them.
	policyIPSetUnresolved = "an IP set doctor does not resolve, counted as every address"
	// policyAllTagged selects every tagged device. serverpro enrolls every
	// server with a tag, so as a destination it reaches every server.
	policyAllTagged = "autogroup:tagged"
	// tailnetIPv4Range and tailnetIPv6Range hold every address Tailscale
	// gives a tailnet device, so a CIDR that covers either one (0.0.0.0/0
	// and ::/0 among them) selects every device.
	tailnetIPv4Range = "100.64.0.0/10"
	tailnetIPv6Range = "fd7a:115c:a1e0::/48"
	// policyAllPortsFromZero and policyAllPortsFromOne are the range
	// spellings of every port.
	policyAllPortsFromZero = "0-65535"
	policyAllPortsFromOne  = "1-65535"
	// policyPortListSeparator splits an ACL destination's port list.
	policyPortListSeparator = ","
	// policyProtocolSeparator splits a grant ip entry of the form
	// <proto>:<ports>.
	policyProtocolSeparator = ":"
)

// A rule allows all when a selector is any spelling of "everything", not only
// the literal wildcard; a policy that writes 100.64.0.0/10, tcp:*, or
// 1-65535 opens the same ports as one that writes "*".
var (
	allSources = []string{policyWildcard, policyAllMembers, policyAllSourcesAnywhere}
	allHosts   = []string{policyWildcard, policyAllTagged}
	allPorts   = []string{policyWildcard, policyAllPortsFromZero, policyAllPortsFromOne}
	// portProtocols are the grant ip protocols, by name and IANA number,
	// whose ports a host's listeners use; every port of one of them is
	// every port UFW guards for that protocol.
	portProtocols = []string{"tcp", "udp", "6", "17"}
	tailnetRanges = []netip.Prefix{netip.MustParsePrefix(tailnetIPv4Range), netip.MustParsePrefix(tailnetIPv6Range)}
)

// PacketPolicy is the network-layer part of a tailnet policy file: the
// first-generation acls and the grants that replace them, plus the hosts
// aliases their selectors may name. The rule sections are pointers because an
// absent section differs from an empty one: Tailscale applies its default
// allow-all policy to a file with neither, while an empty acls list denies
// everything.
type PacketPolicy struct {
	ACLs   *[]ACLRule  `json:"acls"`
	Grants *[]Grant    `json:"grants"`
	Hosts  HostAliases `json:"hosts"`
}

// HostAliases is the policy file's hosts section: names that a rule's
// sources and destinations use in place of an IP address or CIDR, so an
// allow-all range can hide behind a friendly name.
type HostAliases map[string]string

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
			if source, destination, ok := rule.allowsAll(p.Hosts); ok {
				rules = append(rules, fmt.Sprintf("acls[%d] accepts %s to %s", i, source, destination))
			}
		}
	}
	if p.Grants != nil {
		for i, grant := range *p.Grants {
			if source, destination, ports, ok := grant.allowsAll(p.Hosts); ok {
				rules = append(rules, fmt.Sprintf("grants[%d] grants %s to %s on ip %s", i, source, destination, ports))
			}
		}
	}
	return rules
}

// allowsAll returns the matching source and destination, so the finding
// names the spelling the policy file uses.
func (r ACLRule) allowsAll(hosts HostAliases) (string, string, bool) {
	if !strings.EqualFold(r.Action, policyActionAccept) {
		return "", "", false
	}
	source, found := hosts.firstAll(append(append([]string(nil), r.Src...), r.Users...), allSources)
	if !found {
		return "", "", false
	}
	for _, destination := range append(append([]string(nil), r.Dst...), r.Ports...) {
		if spelling, ok := aclDestinationAllowsAll(destination, hosts); ok {
			return source, spelling, true
		}
	}
	return "", "", false
}

func (g Grant) allowsAll(hosts HostAliases) (string, string, string, bool) {
	source, sourceFound := hosts.firstAll(g.Src, allSources)
	destination, destinationFound := hosts.firstAll(g.Dst, allHosts)
	ports, portsFound := firstMatch(g.IP, grantIPAllowsAll)
	return source, destination, ports, sourceFound && destinationFound && portsFound
}

// aclDestinationAllowsAll reads a first-generation <host>:<ports>
// destination. The ports follow the last colon, so an IPv6 host such as
// ::/0 or [::/0] keeps its own colons, and one all-ports entry in a port
// list opens every port.
func aclDestinationAllowsAll(destination string, hosts HostAliases) (string, bool) {
	if destination == policyWildcardHostPort {
		return destination, true
	}
	separator := strings.LastIndex(destination, ":")
	if separator < 0 {
		return "", false
	}
	host := strings.TrimSuffix(strings.TrimPrefix(destination[:separator], "["), "]")
	spelling, found := hosts.firstAll([]string{host}, allHosts)
	if !found {
		return "", false
	}
	if _, found := firstMatch(strings.Split(destination[separator+1:], policyPortListSeparator), isAllPorts); !found {
		return "", false
	}
	// Only an alias changes the spelling; otherwise quote the destination as
	// written, brackets included.
	if spelling != host {
		return spelling + destination[separator:], true
	}
	return destination, true
}

// grantIPAllowsAll reads one grant ip entry: every port alone, which also
// covers UDP and ICMP, or every port of TCP or UDP after the protocol.
func grantIPAllowsAll(entry string) bool {
	if isAllPorts(entry) {
		return true
	}
	protocol, ports, found := strings.Cut(entry, policyProtocolSeparator)
	if !found {
		return false
	}
	return slices.ContainsFunc(portProtocols, func(name string) bool { return strings.EqualFold(name, protocol) }) && isAllPorts(ports)
}

func isAllPorts(ports string) bool {
	return slices.Contains(allPorts, ports)
}

// firstAll returns the first selector that is one of the given spellings of
// everything, a CIDR that covers a tailnet address range, or an IP set,
// after a hosts alias is replaced by its value. A finding through an alias
// or an IP set names both, so the operator sees the name and what doctor
// read behind it.
func (hosts HostAliases) firstAll(selectors, everything []string) (string, bool) {
	for _, selector := range selectors {
		name := strings.TrimSpace(selector)
		value, isAlias := hosts[name]
		resolved := name
		if isAlias {
			resolved = strings.TrimSpace(value)
		}
		// Case-folded, so a spelling Tailscale may read the same way cannot
		// slip past the fail-closed reading.
		ipSet := strings.HasPrefix(strings.ToLower(resolved), policyIPSetPrefix)
		if !ipSet && !slices.Contains(everything, resolved) && !coversTailnet(resolved) {
			continue
		}
		switch {
		case ipSet:
			return fmt.Sprintf("%s (%s)", name, policyIPSetUnresolved), true
		case isAlias:
			return fmt.Sprintf("%s (%s)", name, resolved), true
		}
		return selector, true
	}
	return "", false
}

// coversTailnet reports whether selector is a CIDR that contains a whole
// tailnet address range. A bare address or a narrower range selects only
// some devices, and anything that does not parse as a CIDR is a name.
func coversTailnet(selector string) bool {
	prefix, err := netip.ParsePrefix(selector)
	if err != nil {
		return false
	}
	prefix = prefix.Masked()
	for _, tailnet := range tailnetRanges {
		if prefix.Addr().Is4() == tailnet.Addr().Is4() && prefix.Bits() <= tailnet.Bits() && prefix.Contains(tailnet.Addr()) {
			return true
		}
	}
	return false
}

// firstMatch returns the first entry that allows everything, as the policy
// file spells it, so the finding quotes it.
func firstMatch(entries []string, allowsAll func(string) bool) (string, bool) {
	for _, entry := range entries {
		if allowsAll(strings.TrimSpace(entry)) {
			return entry, true
		}
	}
	return "", false
}
