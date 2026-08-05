package tailscale

import (
	"context"
	"fmt"
)

func (c Client) ValidateSSHPolicy(ctx context.Context, tags []string, adminUser, rootPolicy string) error {
	if rootPolicy != "check-or-disabled" {
		return fmt.Errorf("unsupported root policy %q", rootPolicy)
	}
	p, err := c.Policy(ctx)
	if err != nil {
		return fmt.Errorf("tailscale SSH policy validation failed: %w", err)
	}
	matched := false
	for _, rule := range p.SSH {
		if !ruleMatchesDst(rule, tags) {
			continue
		}
		matched = true
		if contains(rule.Users, "root") && rule.Action != "check" {
			return fmt.Errorf("unsafe Tailscale SSH policy: root access to %v must use check mode", tags)
		}
		if contains(rule.Users, "autogroup:nonroot") && rule.Action != "check" {
			return fmt.Errorf("unsafe Tailscale SSH policy: broad autogroup:nonroot access to %v requires check mode or explicit admin user %q", tags, adminUser)
		}
		if contains(rule.Users, adminUser) || contains(rule.Users, "autogroup:nonroot") {
			return nil
		}
	}
	if !matched {
		return fmt.Errorf("tailnet policy has no SSH rule for destination tags %v", tags)
	}
	return fmt.Errorf("tailnet SSH policy for %v does not allow admin user %q", tags, adminUser)
}

func ruleMatchesDst(rule SSHRule, tags []string) bool {
	for _, tag := range tags {
		if contains(rule.Dst, tag) {
			return true
		}
	}
	return false
}
