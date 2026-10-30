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
			if rule.allowsAll() {
				rules = append(rules, fmt.Sprintf("acls[%d] accepts %s to %s", i, policyWildcard, policyWildcardHostPort))
			}
		}
	}
	if p.Grants != nil {
		for i, grant := range *p.Grants {
			if grant.allowsAll() {
				rules = append(rules, fmt.Sprintf("grants[%d] grants %s to %s on ip %s", i, policyWildcard, policyWildcard, policyWildcard))
			}
		}
	}
	return rules
}

func (r ACLRule) allowsAll() bool {
	if !strings.EqualFold(r.Action, policyActionAccept) {
		return false
	}
	sources := append(append([]string(nil), r.Src...), r.Users...)
	destinations := append(append([]string(nil), r.Dst...), r.Ports...)
	return slices.Contains(sources, policyWildcard) && slices.Contains(destinations, policyWildcardHostPort)
}

func (g Grant) allowsAll() bool {
	return slices.Contains(g.Src, policyWildcard) && slices.Contains(g.Dst, policyWildcard) && slices.Contains(g.IP, policyWildcard)
}
