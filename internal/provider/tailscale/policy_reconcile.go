package tailscale

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/sagmans/serverpro/internal/mesh"
)

const (
	serverproTagPrefix = "tag:serverpro-"
	tailnetTagPrefix   = "tag:"
)

var ErrPolicyReconcilePlanChanged = errors.New("tailnet policy reconcile plan changed; rerun preview")

// ServerproPolicyReconcilePlan identifies tailnet-global policy entries that
// have no registered-state or live-device evidence.
type ServerproPolicyReconcilePlan = mesh.PolicyReconcilePlan

// PlanServerproPolicyReconcile reads live devices and policy without mutation.
func (c Client) PlanServerproPolicyReconcile(ctx context.Context, protectedTags []string) (ServerproPolicyReconcilePlan, error) {
	plan, _, err := c.serverproPolicyReconcilePlan(ctx, protectedTags)
	return plan, err
}

// ApplyServerproPolicyReconcile publishes only the removals approved from a
// preview; fresh evidence must produce the identical plan before mutation.
func (c Client) ApplyServerproPolicyReconcile(ctx context.Context, protectedTags []string, approved ServerproPolicyReconcilePlan) error {
	plan, file, err := c.serverproPolicyReconcilePlan(ctx, protectedTags)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(plan, approved) {
		return ErrPolicyReconcilePlanChanged
	}
	if plan.Empty() {
		return nil
	}
	if _, err := file.doc.removeTagOwners(plan.TagOwners); err != nil {
		return err
	}
	if err := rewritePlannedSSHRules(file.doc, plan.SSHRules); err != nil {
		return err
	}
	return c.publishPolicyFile(ctx, file)
}

func (c Client) serverproPolicyReconcilePlan(ctx context.Context, protectedTags []string) (ServerproPolicyReconcilePlan, policyFile, error) {
	devices, err := c.Devices(ctx)
	if err != nil {
		return ServerproPolicyReconcilePlan{}, policyFile{}, err
	}
	file, err := c.readPolicyFile(ctx)
	if err != nil {
		return ServerproPolicyReconcilePlan{}, policyFile{}, err
	}
	doc := file.doc
	used := make(map[string]bool, len(protectedTags))
	for _, tag := range protectedTags {
		used[tag] = true
	}
	for _, device := range devices {
		for _, tag := range device.Tags {
			used[tag] = true
		}
	}
	owners, err := doc.tagOwners()
	if err != nil {
		return ServerproPolicyReconcilePlan{}, policyFile{}, err
	}
	// Owner references are policy dependencies even without device or state evidence.
	for _, values := range owners {
		for _, value := range values {
			if strings.HasPrefix(value, tailnetTagPrefix) {
				used[value] = true
			}
		}
	}
	removable := map[string]bool{}
	plan := ServerproPolicyReconcilePlan{}
	for tag, values := range owners {
		if !used[tag] && strings.HasPrefix(tag, serverproTagPrefix) && sameStringSet(values, []string{serverproPolicyOwner}) {
			removable[tag] = true
			plan.TagOwners = append(plan.TagOwners, tag)
		}
	}
	sort.Strings(plan.TagOwners)
	rules, err := doc.sshRules()
	if err != nil {
		return ServerproPolicyReconcilePlan{}, policyFile{}, err
	}
	for _, rule := range rules {
		if planned, ok := plannedServerproSSHRule(rule, removable); ok {
			plan.SSHRules = append(plan.SSHRules, planned)
		}
	}
	return plan, file, nil
}

// plannedServerproSSHRule recognizes the rule shapes that serverpro writes: the
// human rule with either action and the automation accept rule. When the
// automation tag itself is stale, the whole accept rule goes, because a rule
// whose source has no tag owner fails policy validation.
func plannedServerproSSHRule(rule SSHRule, removable map[string]bool) (SSHRule, bool) {
	if len(rule.Dst) == 0 || len(rule.Users) != 1 || rule.Users[0] == "" {
		return SSHRule{}, false
	}
	automation := isAutomationRuleShape(rule)
	if automation && removable[mesh.AutomationSourceTag] {
		return rule, true
	}
	if !automation && !isHumanRuleShape(rule) {
		return SSHRule{}, false
	}
	stale := make([]string, 0, len(rule.Dst))
	for _, tag := range rule.Dst {
		if removable[tag] {
			stale = append(stale, tag)
		}
	}
	if len(stale) == 0 {
		return SSHRule{}, false
	}
	planned := rule
	planned.Dst = stale
	return planned, true
}

// isHumanRuleShape never counts a root accept rule as serverpro's, because
// serverpro refuses to write one: that rule is the operator's to review.
func isHumanRuleShape(rule SSHRule) bool {
	if !sameStringSet(rule.Src, []string{serverproPolicyOwner}) {
		return false
	}
	return rule.Action == sshActionCheck || (rule.Action == sshActionAccept && rule.Users[0] != mesh.RootSSHUser)
}

func isAutomationRuleShape(rule SSHRule) bool {
	return rule.Action == sshActionAccept && sameStringSet(rule.Src, []string{mesh.AutomationSourceTag}) && rule.Users[0] != mesh.RootSSHUser
}

func rewritePlannedSSHRules(doc policyDocument, planned []SSHRule) error {
	rules, err := doc.sshRules()
	if err != nil {
		return err
	}
	out := make([]SSHRule, 0, len(rules))
	for _, rule := range rules {
		removable := plannedDestinations(rule, planned)
		if len(removable) == 0 {
			out = append(out, rule)
			continue
		}
		destinations := make([]string, 0, len(rule.Dst))
		for _, destination := range rule.Dst {
			if !removable[destination] {
				destinations = append(destinations, destination)
			}
		}
		if len(destinations) == 0 {
			continue
		}
		rule.Dst = destinations
		out = append(out, rule)
	}
	return doc.setSSHRules(out)
}

func plannedDestinations(rule SSHRule, planned []SSHRule) map[string]bool {
	removable := map[string]bool{}
	for _, candidate := range planned {
		if candidate.Action != rule.Action || !sameStringSet(candidate.Src, rule.Src) || !sameStringSet(candidate.Users, rule.Users) {
			continue
		}
		for _, destination := range candidate.Dst {
			removable[destination] = true
		}
	}
	return removable
}
