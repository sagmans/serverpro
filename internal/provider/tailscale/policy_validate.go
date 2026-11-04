package tailscale

import (
	"context"
	"fmt"
)

// ValidateSSHPolicy reads the live policy after a write and fails closed when
// root or broad nonroot access to the server tags skips check mode, when no
// rule lets the admin user in, or when automation is on but its accept rule
// is missing (unattended runs would then hang on a browser login), or when
// config asks for a human accept rule and its rule still asks for the browser
// login. The unsafe rule scan covers every device tag, because a rule on any of
// them reaches the server; the accept rule must be the one for sshTags that
// serverpro wrote.
func (c Client) ValidateSSHPolicy(ctx context.Context, tags, sshTags []string, adminUser, rootPolicy, automationSource, humanAction string) error {
	if rootPolicy != "check-or-disabled" {
		return fmt.Errorf("unsupported root policy %q", rootPolicy)
	}
	p, err := c.Policy(ctx)
	if err != nil {
		return fmt.Errorf("tailscale SSH policy validation failed: %w", err)
	}
	scan, err := scanSSHRules(p.SSH, tags, sshTags, adminUser, automationSource)
	if err != nil {
		return err
	}
	switch {
	case !scan.matched:
		return fmt.Errorf("tailnet policy has no SSH rule for destination tags %v", tags)
	case !scan.allowsAdmin:
		return fmt.Errorf("tailnet SSH policy for %v does not allow admin user %q", tags, adminUser)
	case automationSource != "" && !scan.automationAllowed:
		return fmt.Errorf("tailnet SSH policy for %v has no accept rule for admin user %q from %s", sshTags, adminUser, automationSource)
	case humanAction == sshActionAccept && !scan.humanAccepted:
		return fmt.Errorf("tailnet SSH policy for %v has no accept rule for admin user %q from %s", sshTags, adminUser, serverproPolicyOwner)
	}
	return nil
}

// sshRuleScan is what the read-back learned from the rules that reach a server.
type sshRuleScan struct {
	matched, allowsAdmin, automationAllowed, humanAccepted bool
}

// scanSSHRules stops at the first unsafe rule, because the read-back fails
// closed on it whatever the other rules allow.
func scanSSHRules(rules []SSHRule, tags, sshTags []string, adminUser, automationSource string) (sshRuleScan, error) {
	var scan sshRuleScan
	for _, rule := range rules {
		if !ruleMatchesDst(rule, tags) {
			continue
		}
		scan.matched = true
		if err := unsafeSSHRule(rule, tags, adminUser); err != nil {
			return scan, err
		}
		scan.allowsAdmin = scan.allowsAdmin || contains(rule.Users, adminUser) || contains(rule.Users, "autogroup:nonroot")
		scan.automationAllowed = scan.automationAllowed || (automationSource != "" && automationSSHRuleMatches(rule, sshTags, adminUser, automationSource))
		scan.humanAccepted = scan.humanAccepted || (rule.Action == sshActionAccept && serverproSSHRuleMatches(rule, sshTags, adminUser))
	}
	return scan, nil
}

// unsafeSSHRule refuses a rule that lets root, or every nonroot user, in
// without check mode: those logins must always ask for a fresh browser login.
func unsafeSSHRule(rule SSHRule, tags []string, adminUser string) error {
	if rule.Action == sshActionCheck {
		return nil
	}
	if contains(rule.Users, "root") {
		return fmt.Errorf("unsafe Tailscale SSH policy: root access to %v must use check mode", tags)
	}
	if contains(rule.Users, "autogroup:nonroot") {
		return fmt.Errorf("unsafe Tailscale SSH policy: broad autogroup:nonroot access to %v requires check mode or explicit admin user %q", tags, adminUser)
	}
	return nil
}

func ruleMatchesDst(rule SSHRule, tags []string) bool {
	for _, tag := range tags {
		if contains(rule.Dst, tag) {
			return true
		}
	}
	return false
}
