package tailscale

import (
	"encoding/json"

	"github.com/sagmans/serverpro/internal/mesh"
)

const (
	sshActionCheck  = mesh.SSHActionCheck
	sshActionAccept = mesh.SSHActionAccept
)

// ensureSSHRule makes the human rule for these tags carry action. It changes
// the action in place, so fields that serverpro does not model stay, and it
// drops other copies: Tailscale applies check rules before accept rules, so a
// left-over check copy would keep the browser login after a change to accept.
func (d policyDocument) ensureSSHRule(tags []string, adminUser, action string) (bool, error) {
	rules, err := d.sshRules()
	if err != nil {
		return false, err
	}
	out := make([]SSHRule, 0, len(rules)+1)
	found, changed := false, false
	for _, rule := range rules {
		if !serverproSSHRuleMatches(rule, tags, adminUser) {
			out = append(out, rule)
			continue
		}
		if found {
			changed = true
			continue
		}
		found = true
		changed = changed || rule.Action != action
		rule.Action = action
		out = append(out, rule)
	}
	if !found {
		out = append(out, serverproSSHRule(tags, adminUser, action))
		changed = true
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

// serverproSSHRule is the human rule. Its action comes from config: check by
// default, so a person on an admin device does a fresh browser login, or accept
// when the operator gave up that step-up auth.
func serverproSSHRule(tags []string, adminUser, action string) SSHRule {
	return SSHRule{Action: action, Src: []string{serverproPolicyOwner}, Dst: append([]string(nil), tags...), Users: []string{adminUser}}
}

// serverproSSHRuleMatches recognizes the human rule with either action, so a
// config change rewrites the rule instead of adding a second one.
func serverproSSHRuleMatches(rule SSHRule, tags []string, adminUser string) bool {
	return (rule.Action == sshActionCheck || rule.Action == sshActionAccept) && sameStringSet(rule.Src, []string{serverproPolicyOwner}) && sameStringSet(rule.Dst, tags) && sameStringSet(rule.Users, []string{adminUser})
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
