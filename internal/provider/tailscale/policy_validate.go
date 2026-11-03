package tailscale

import (
	"context"
	"fmt"
)

// ValidateSSHPolicy reads the live policy after a write and fails closed when
// root or broad nonroot access to the server tags skips check mode, when no
// rule lets the admin user in, or when automation is on but its accept rule
// is missing (unattended runs would then hang on a browser login).
func (c Client) ValidateSSHPolicy(ctx context.Context, tags []string, adminUser, rootPolicy, automationSource string) error {
	if rootPolicy != "check-or-disabled" {
		return fmt.Errorf("unsupported root policy %q", rootPolicy)
	}
	p, err := c.Policy(ctx)
	if err != nil {
		return fmt.Errorf("tailscale SSH policy validation failed: %w", err)
	}
	matched, allowsAdmin, automationAllowed := false, false, false
	for _, rule := range p.SSH {
		if !ruleMatchesDst(rule, tags) {
			continue
		}
		matched = true
		if contains(rule.Users, "root") && rule.Action != sshActionCheck {
			return fmt.Errorf("unsafe Tailscale SSH policy: root access to %v must use check mode", tags)
		}
		if contains(rule.Users, "autogroup:nonroot") && rule.Action != sshActionCheck {
			return fmt.Errorf("unsafe Tailscale SSH policy: broad autogroup:nonroot access to %v requires check mode or explicit admin user %q", tags, adminUser)
		}
		if contains(rule.Users, adminUser) || contains(rule.Users, "autogroup:nonroot") {
			allowsAdmin = true
		}
		if automationSource != "" && automationSSHRuleMatches(rule, tags, adminUser, automationSource) {
			automationAllowed = true
		}
	}
	switch {
	case !matched:
		return fmt.Errorf("tailnet policy has no SSH rule for destination tags %v", tags)
	case !allowsAdmin:
		return fmt.Errorf("tailnet SSH policy for %v does not allow admin user %q", tags, adminUser)
	case automationSource != "" && !automationAllowed:
		return fmt.Errorf("tailnet SSH policy for %v has no accept rule for admin user %q from %s", tags, adminUser, automationSource)
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
