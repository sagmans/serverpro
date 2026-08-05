package tailscale

import (
	"encoding/json"
	"fmt"
	"slices"
)

func (d policyDocument) ensureSSHRule(tags []string, adminUser string) (bool, error) {
	rules, err := d.sshRules()
	if err != nil {
		return false, err
	}
	for _, rule := range rules {
		if serverproSSHRuleMatches(rule, tags, adminUser) {
			return false, nil
		}
	}
	rules = append(rules, serverproSSHRule(tags, adminUser))
	return true, d.setSSHRules(rules)
}

func (d policyDocument) inspectSSHRule(tags []string, adminUser string) (bool, error) {
	rules, err := d.sshRules()
	if err != nil {
		return false, err
	}
	for _, rule := range rules {
		if serverproSSHRuleMatches(rule, tags, adminUser) {
			return true, nil
		}
	}
	for _, rule := range rules {
		if serverproSSHRuleMayBeDrifted(rule, tags, adminUser) {
			return false, fmt.Errorf("tailscale policy ownership drift for SSH rule tags %v user %q", tags, adminUser)
		}
	}
	return false, nil
}

func (d policyDocument) removeSSHRule(tags []string, adminUser string) (bool, error) {
	present, err := d.inspectSSHRule(tags, adminUser)
	if err != nil || !present {
		return false, err
	}
	rules, err := d.sshRules()
	if err != nil {
		return false, err
	}
	out := rules[:0]
	for _, rule := range rules {
		if !serverproSSHRuleMatches(rule, tags, adminUser) {
			out = append(out, rule)
		}
	}
	return true, d.setSSHRules(out)
}

func (d policyDocument) sshRules() ([]SSHRule, error) {
	var rules []SSHRule
	raw, ok := d["ssh"]
	if !ok || len(raw) == 0 {
		return rules, nil
	}
	return rules, json.Unmarshal(raw, &rules)
}

func (d policyDocument) setSSHRules(rules []SSHRule) error {
	if len(rules) == 0 {
		delete(d, "ssh")
		return nil
	}
	b, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	d["ssh"] = b
	return nil
}

func serverproSSHRule(tags []string, adminUser string) SSHRule {
	return SSHRule{Action: "check", Src: []string{serverproPolicyOwner}, Dst: append([]string(nil), tags...), Users: []string{adminUser}}
}

func serverproSSHRuleMatches(rule SSHRule, tags []string, adminUser string) bool {
	return rule.Action == "check" && sameStringSet(rule.Src, []string{serverproPolicyOwner}) && sameStringSet(rule.Dst, tags) && sameStringSet(rule.Users, []string{adminUser})
}

func serverproSSHRuleMayBeDrifted(rule SSHRule, tags []string, adminUser string) bool {
	matches := 0
	if rule.Action == "check" {
		matches++
	}
	if slices.Contains(rule.Src, serverproPolicyOwner) {
		matches++
	}
	destinationsMatch := len(tags) > 0
	for _, tag := range tags {
		if !slices.Contains(rule.Dst, tag) {
			destinationsMatch = false
			break
		}
	}
	if destinationsMatch {
		matches++
	}
	if slices.Contains(rule.Users, adminUser) {
		matches++
	}
	// Three matching identity dimensions distinguish likely mutation of the
	// tracked rule from unrelated policy entries while still failing closed.
	return matches >= 3
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}
