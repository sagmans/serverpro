package doctor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	tailnetSSHPolicyCheckName = "tailnet ssh policy"
	// tailnetSSHPolicyTimeout matches the packet policy read: one small API
	// call each for the read and the optional rewrite.
	tailnetSSHPolicyTimeout = 10 * time.Second
	// tailnetSSHPolicyFixTimeout also covers the wait for the tailnet policy
	// lock, a create or reconcile on the same tailnet can hold it.
	tailnetSSHPolicyFixTimeout = time.Minute
	tailnetSSHPolicyFixHint    = "run serverpro server doctor NAME --fix; it rewrites only the serverpro check and automation accept rules"
	// tailnetSSHForeignHint covers rules that serverpro did not write. Doctor
	// never edits them, because the policy is shared by the whole tailnet.
	tailnetSSHForeignHint = "edit the tailnet policy by hand: give accept to the admin user only from " + mesh.AutomationSourceTag + ", and keep root on check"
	tailnetSSHRevokeHint  = "if no other server with these tags and admin user has automation on, run serverpro server ssh-policy NAME"
	sshActionCheck        = "check"
	sshActionAccept       = "accept"
	sshUserRoot           = "root"
	sshUserNonRoot        = "autogroup:nonroot"
)

var errTailnetSSHPolicyUnsupported = errors.New("tailnet SSH policy introspection not supported")

// sshPolicyFinding is one difference between the live SSH rules and config.
// fixable marks the ones that EnsureServerproPolicy repairs; severe marks the
// ones that leave access open without step-up auth, or block unattended runs.
type sshPolicyFinding struct {
	text    string
	fixable bool
	severe  bool
	// hint replaces the default remediation for a finding that needs another
	// command than --fix or a hand edit.
	hint string
}

// checkTailnetSSHPolicy compares the live SSH rules for this server's tags
// with what config and state ask for: the human check rule, the automation
// accept rule only when automation is on, and root on check. It runs before
// the remote checks so that --fix can add a missing accept rule before the
// first unattended session needs it. The fixer that the CLI gives doctor
// takes the tailnet lock, records state and reads the policy back, so the
// check itself stays free of locks and files.
func checkTailnetSSHPolicy(ctx context.Context, cfg config.Config, st state.State, token string, client TailscaleClient, fix bool) Result {
	if token == "" {
		return skip("provider", tailnetSSHPolicyCheckName, "no API token; auth-key-only mode")
	}
	policyClient, ok := client.(TailnetSSHPolicyClient)
	if !ok {
		return skip("provider", tailnetSSHPolicyCheckName, errTailnetSSHPolicyUnsupported.Error())
	}
	ts := cfg.Access.Tailscale
	readCtx, cancel := context.WithTimeout(ctx, tailnetSSHPolicyTimeout)
	rules, err := policyClient.SSHPolicy(readCtx)
	cancel()
	if errors.Is(err, errTailnetSSHPolicyUnsupported) {
		return skip("provider", tailnetSSHPolicyCheckName, err.Error())
	}
	if err != nil {
		result := warn("provider", tailnetSSHPolicyCheckName, "policy fetch failed: "+err.Error())
		result.Remediation = "grant the Tailscale API token policy-file read access, then rerun doctor"
		return result
	}
	findings := sshPolicyFindings(rules, ts.Tags, ts.SSHRuleTags(), cfg.Admin.Username, ts.AutomationSource())
	if recorded, want := st.Tailscale.PolicySSHAutomationSource, ts.AutomationSource(); recorded != want {
		findings = append(findings, sshPolicyFinding{text: fmt.Sprintf("state records automation source %q, config asks for %q", recorded, want), fixable: true})
	}
	if len(findings) == 0 {
		return pass("provider", tailnetSSHPolicyCheckName, sshPolicyIntent(cfg))
	}
	if fix && slices.ContainsFunc(findings, func(f sshPolicyFinding) bool { return f.fixable }) {
		return repairTailnetSSHPolicy(ctx, cfg, client, findings)
	}
	return sshPolicyResult(findings)
}

// repairTailnetSSHPolicy calls the same policy write as provision, so a doctor
// repair and a create leave the same rules and the same state.
func repairTailnetSSHPolicy(ctx context.Context, cfg config.Config, client TailscaleClient, findings []sshPolicyFinding) Result {
	fixer, ok := client.(TailnetSSHPolicyFixer)
	if !ok {
		return sshPolicyResult(findings)
	}
	ts := cfg.Access.Tailscale
	fixCtx, cancel := context.WithTimeout(ctx, tailnetSSHPolicyFixTimeout)
	_, err := fixer.EnsureServerproPolicy(fixCtx, ts.Tags, ts.SSHRuleTags(), cfg.Admin.Username, ts.RootPolicy, ts.AutomationSource())
	cancel()
	if err != nil {
		return fail("provider", tailnetSSHPolicyCheckName, "repair failed: "+err.Error()+"; findings: "+findingsText(findings), tailnetSSHPolicyFixHint)
	}
	fixed, remaining := splitFixable(findings)
	if len(remaining) > 0 {
		result := sshPolicyResult(remaining)
		result.Evidence = "fixed: " + findingsText(fixed) + "; remaining: " + result.Evidence
		return result
	}
	return pass("provider", tailnetSSHPolicyCheckName, "fixed: "+findingsText(fixed)+"; now "+sshPolicyIntent(cfg))
}

func splitFixable(findings []sshPolicyFinding) (fixed, remaining []sshPolicyFinding) {
	for _, f := range findings {
		if f.fixable {
			fixed = append(fixed, f)
		} else {
			remaining = append(remaining, f)
		}
	}
	return fixed, remaining
}

// sshPolicyFindings scans every rule that reaches the server through any of
// its device tags, because Tailscale applies a rule to every node with any
// dst tag. Only the rules for sshTags count as this server's own; an
// automation rule that reaches it through a shared tag is reported.
func sshPolicyFindings(rules []mesh.SSHRule, tags, sshTags []string, adminUser, source string) []sshPolicyFinding {
	var findings []sshPolicyFinding
	humanRule, automationRule := false, false
	for _, rule := range rules {
		if !sshRuleTargets(rule, tags) {
			continue
		}
		switch {
		case isServerproCheckRule(rule, sshTags, adminUser):
			humanRule = true
		case isAutomationAcceptRule(rule, sshTags, adminUser):
			automationRule = true
		case slices.Contains(rule.Src, mesh.AutomationSourceTag):
			// A rule from the automation tag in another shape (old tags,
			// another server's tags or admin user) keeps access open that no
			// config of this server asks for.
			findings = append(findings, sshPolicyFinding{text: fmt.Sprintf("other %s rule from %s for %v to %v", rule.Action, mesh.AutomationSourceTag, rule.Users, rule.Dst)})
		}
		findings = append(findings, sshRuleRiskFindings(rule, adminUser)...)
	}
	if !humanRule {
		findings = append(findings, sshPolicyFinding{text: "human check rule for " + adminUser + " missing", fixable: true})
	}
	switch {
	case source != "" && !automationRule:
		findings = append(findings, sshPolicyFinding{text: "automation accept rule for " + adminUser + " missing; unattended runs need a browser login", fixable: true, severe: true})
	case source == "" && automationRule:
		// Not fixable here and not severe: another server with the same tags
		// and admin user can own this shared rule. ssh-policy checks the
		// registry before it revokes.
		findings = append(findings, sshPolicyFinding{text: "automation accept rule for " + adminUser + " present but automation is off for this server; if no other server with these tags uses it, revoke it with serverpro server ssh-policy NAME", hint: tailnetSSHRevokeHint})
	}
	return findings
}

// sshRuleRiskFindings reports access that one rule opens without step-up
// auth: root outside check mode (severe), and accept for the admin user from
// any source other than the automation tag.
func sshRuleRiskFindings(rule mesh.SSHRule, adminUser string) []sshPolicyFinding {
	var findings []sshPolicyFinding
	if slices.Contains(rule.Users, sshUserRoot) && rule.Action != sshActionCheck {
		findings = append(findings, sshPolicyFinding{text: fmt.Sprintf("root %s rule from %v", rule.Action, rule.Src), severe: true})
	}
	if rule.Action == sshActionAccept && grantsAdmin(rule.Users, adminUser) && !slices.Equal(rule.Src, []string{mesh.AutomationSourceTag}) {
		findings = append(findings, sshPolicyFinding{text: fmt.Sprintf("accept rule for %v from %v, not from %s", rule.Users, rule.Src, mesh.AutomationSourceTag)})
	}
	return findings
}

// grantsAdmin is true for a user list that lets a session log in as the admin
// user, by name or through autogroup:nonroot.
func grantsAdmin(users []string, adminUser string) bool {
	return slices.Contains(users, adminUser) || slices.Contains(users, sshUserNonRoot)
}

// sshRuleTargets matches a rule that names any of the server tags, because
// Tailscale applies a rule to every destination in its list.
func sshRuleTargets(rule mesh.SSHRule, tags []string) bool {
	return slices.ContainsFunc(rule.Dst, func(dst string) bool { return slices.Contains(tags, dst) })
}

func isServerproCheckRule(rule mesh.SSHRule, tags []string, adminUser string) bool {
	return rule.Action == sshActionCheck && sameSet(rule.Src, []string{mesh.ServerproPolicyOwner}) && sameSet(rule.Dst, tags) && sameSet(rule.Users, []string{adminUser})
}

func isAutomationAcceptRule(rule mesh.SSHRule, tags []string, adminUser string) bool {
	return rule.Action == sshActionAccept && sameSet(rule.Src, []string{mesh.AutomationSourceTag}) && sameSet(rule.Dst, tags) && sameSet(rule.Users, []string{adminUser})
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func allFixable(findings []sshPolicyFinding) bool {
	return !slices.ContainsFunc(findings, func(f sshPolicyFinding) bool { return !f.fixable })
}

// sshPolicyResult fails on findings that open access without step-up auth or
// stall unattended runs, and warns on the rest.
func sshPolicyResult(findings []sshPolicyFinding) Result {
	hint := tailnetSSHPolicyFixHint
	if !allFixable(findings) {
		hint = tailnetSSHForeignHint
		if i := slices.IndexFunc(findings, func(f sshPolicyFinding) bool { return !f.fixable && f.hint != "" }); i >= 0 {
			hint = findings[i].hint
		}
	}
	if slices.ContainsFunc(findings, func(f sshPolicyFinding) bool { return f.severe }) {
		return fail("provider", tailnetSSHPolicyCheckName, findingsText(findings), hint)
	}
	result := warn("provider", tailnetSSHPolicyCheckName, findingsText(findings))
	result.Remediation = hint
	return result
}

func findingsText(findings []sshPolicyFinding) string {
	texts := make([]string, 0, len(findings))
	for _, f := range findings {
		texts = append(texts, f.text)
	}
	return strings.Join(texts, "; ")
}

func sshPolicyIntent(cfg config.Config) string {
	source := cfg.Access.Tailscale.AutomationSource()
	if source == "" {
		return "check rule for " + cfg.Admin.Username + "; automation off"
	}
	return "check rule for " + cfg.Admin.Username + "; accept from " + source
}
