package tailscale

import (
	"encoding/json"

	"github.com/sagmans/serverpro/internal/mesh"
)

// Tailscale SSH rule actions that serverpro writes. check asks for a recent
// browser login; accept does not.
const (
	sshActionCheck  = "check"
	sshActionAccept = "accept"
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

// ensureAutomationSSHRule adds the accept rule for the automation source when
// source is set, and drops exact duplicates of it. With source "" it changes
// nothing: the rule can belong to another server with the same tags, so only
// removeAutomationSSHRule, after a registry check, may revoke it.
func (d policyDocument) ensureAutomationSSHRule(tags []string, adminUser, source string) (added, removed bool, err error) {
	if source == "" {
		return false, false, nil
	}
	rules, err := d.sshRules()
	if err != nil {
		return false, false, err
	}
	out := make([]SSHRule, 0, len(rules)+1)
	found := false
	for _, rule := range rules {
		if !automationSSHRuleMatches(rule, tags, adminUser, mesh.AutomationSourceTag) {
			out = append(out, rule)
			continue
		}
		if found {
			removed = true
			continue
		}
		found = true
		out = append(out, rule)
	}
	if !found {
		out = append(out, automationSSHRule(tags, adminUser, source))
		added = true
	}
	if !added && !removed {
		return false, false, nil
	}
	return added, removed, d.setSSHRules(out)
}

// removeAutomationSSHRule deletes every exact copy of the automation accept
// rule for these tags and admin user. A left-over accept rule keeps unattended
// access open after the operator turned automation off.
func (d policyDocument) removeAutomationSSHRule(tags []string, adminUser string) (bool, error) {
	rules, err := d.sshRules()
	if err != nil {
		return false, err
	}
	out := make([]SSHRule, 0, len(rules))
	for _, rule := range rules {
		if !automationSSHRuleMatches(rule, tags, adminUser, mesh.AutomationSourceTag) {
			out = append(out, rule)
		}
	}
	if len(out) == len(rules) {
		return false, nil
	}
	return true, d.setSSHRules(out)
}

// serverproSSHRule is the human rule. It stays check mode in every config, so
// a person on an admin device always does a fresh browser login.
func serverproSSHRule(tags []string, adminUser string) SSHRule {
	return SSHRule{Action: sshActionCheck, Src: []string{serverproPolicyOwner}, Dst: append([]string(nil), tags...), Users: []string{adminUser}}
}

func serverproSSHRuleMatches(rule SSHRule, tags []string, adminUser string) bool {
	return rule.Action == sshActionCheck && sameStringSet(rule.Src, []string{serverproPolicyOwner}) && sameStringSet(rule.Dst, tags) && sameStringSet(rule.Users, []string{adminUser})
}

// automationSSHRule gives accept to the admin user only, never root, and only
// from the one tagged source, so human devices keep step-up auth.
func automationSSHRule(tags []string, adminUser, source string) SSHRule {
	return SSHRule{Action: sshActionAccept, Src: []string{source}, Dst: append([]string(nil), tags...), Users: []string{adminUser}}
}

func automationSSHRuleMatches(rule SSHRule, tags []string, adminUser, source string) bool {
	return rule.Action == sshActionAccept && sameStringSet(rule.Src, []string{source}) && sameStringSet(rule.Dst, tags) && sameStringSet(rule.Users, []string{adminUser})
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
