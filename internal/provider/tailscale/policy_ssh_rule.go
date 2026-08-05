package tailscale

import "encoding/json"

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

func (d policyDocument) removeSSHRule(tags []string, adminUser string) (bool, error) {
	rules, err := d.sshRules()
	if err != nil {
		return false, err
	}
	out := rules[:0]
	changed := false
	for _, rule := range rules {
		if serverproSSHRuleMatches(rule, tags, adminUser) {
			changed = true
			continue
		}
		out = append(out, rule)
	}
	if !changed {
		return false, nil
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
