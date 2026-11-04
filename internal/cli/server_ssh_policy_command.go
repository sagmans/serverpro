package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/spf13/cobra"
)

// serverSSHPolicyRow reports the rules that one ssh-policy run asked for and
// what it changed, so an operator can audit a migration from the output.
type serverSSHPolicyRow struct {
	Status    string   `json:"status"`
	DryRun    bool     `json:"dry_run,omitempty"`
	Namespace string   `json:"namespace"`
	Server    string   `json:"server"`
	Tags      []string `json:"tags"`
	// SSHTags is the destination of the rules, so the output shows when a
	// server narrows them to its own tag.
	SSHTags          []string `json:"ssh_tags"`
	AdminUser        string   `json:"admin_user"`
	AutomationSource string   `json:"automation_source,omitempty"`
	// HumanAction is the action that config asks for on the human rule, so a
	// migration shows which step-up auth stays.
	HumanAction              string   `json:"human_action"`
	TagOwnersAdded           []string `json:"tag_owners_added,omitempty"`
	HumanRuleChanged         bool     `json:"human_rule_changed,omitempty"`
	AutomationSSHRuleAdded   bool     `json:"automation_rule_added,omitempty"`
	AutomationSSHRuleRemoved bool     `json:"automation_rule_removed,omitempty"`
	// AutomationSSHRuleKeptFor names the registered servers that still use
	// the shared accept rule, so the operator sees why a revoke did not run.
	AutomationSSHRuleKeptFor []string `json:"automation_rule_kept_for,omitempty"`
}

// serverSSHPolicyClient is the part of the tailnet adapter that ssh-policy
// and the doctor repair need; services.go builds it, so this file stays
// provider-neutral.
type serverSSHPolicyClient interface {
	EnsureServerproPolicy(ctx context.Context, tags, sshTags []string, adminUser, rootPolicy, automationSource, humanAction string) (mesh.PolicyChange, error)
	RemoveAutomationSSHRule(ctx context.Context, tags []string, adminUser string) (mesh.PolicyChange, error)
	ValidateSSHPolicy(ctx context.Context, tags, sshTags []string, adminUser, rootPolicy, automationSource, humanAction string) error
}

// serverSSHPolicyCmd applies only the serverpro SSH rules for one server. It
// exists so that turning automation on or off for an existing server touches
// the tailnet policy and nothing else; doctor --fix also changes the host.
func (a *app) serverSSHPolicyCmd() *cobra.Command {
	return withScopedFlags(&cobra.Command{Use: "ssh-policy NAME", Short: "apply the serverpro Tailscale SSH rules from config", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerSSHPolicy(cmd.Context(), args[0])
	}}, "config", "state", "namespace", "provider", "non-interactive", "dry-run")
}

func (a *app) runServerSSHPolicy(ctx context.Context, name string) error {
	cfg, _, st, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return err
	}
	row := serverSSHPolicyRow{Namespace: cfg.Namespace, Server: targetServer(cfg.Server), Tags: cfg.Access.Tailscale.Tags, SSHTags: cfg.Access.Tailscale.SSHRuleTags(), AdminUser: cfg.Admin.Username, AutomationSource: cfg.Access.Tailscale.AutomationSource(), HumanAction: cfg.Access.Tailscale.HumanSSHAction()}
	if a.dryRun {
		row.Status, row.DryRun = "planned", true
		return writeJSON(a.stdout, row)
	}
	unlockWorkflow, err := lockServerArtifactWorkflow(ctx, st)
	if err != nil {
		return err
	}
	defer unlockWorkflow()
	cfg, stPath, _, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return err
	}
	creds, _, err := a.ensureCredentials(cfg)
	if err != nil {
		return err
	}
	if creds.Tailscale == "" {
		return fmt.Errorf("ssh-policy for %s/%s needs a Tailscale API token in the server credentials", cfg.Namespace, row.Server)
	}
	operationCtx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	client := a.newServerSSHPolicyClient(creds.Tailscale, cfg.Access.Tailscale.Tailnet)
	change, keptFor, err := applyServerSSHPolicy(operationCtx, cfg, stPath, client, true)
	if err != nil {
		return err
	}
	row.Status = "applied"
	row.TagOwnersAdded, row.HumanRuleChanged = change.TagOwners, change.SSHRule
	row.AutomationSSHRuleAdded, row.AutomationSSHRuleRemoved = change.AutomationSSHRule, change.AutomationSSHRuleRemoved
	row.AutomationSSHRuleKeptFor = keptFor
	return writeJSON(a.stdout, row)
}

// applyServerSSHPolicy is the one locked write path for the serverpro SSH
// rules outside provision: ssh-policy and doctor --fix both use it. It holds
// the tailnet policy lock so a concurrent reconcile cannot see a new rule
// before state records it, records state after each write and before the
// read-back, so a failed validation never leaves a live rule unrecorded, and
// revokes the shared accept rule only when revoke is set and no other
// registered server still records it.
func applyServerSSHPolicy(ctx context.Context, cfg config.Config, stPath string, client serverSSHPolicyClient, revoke bool) (mesh.PolicyChange, []string, error) {
	ts := cfg.Access.Tailscale
	unlockTailnet, err := state.LockTailnetPolicy(ctx, config.RegistryPath(), ts.Tailnet)
	if err != nil {
		return mesh.PolicyChange{}, nil, err
	}
	defer unlockTailnet()
	change, err := client.EnsureServerproPolicy(ctx, ts.Tags, ts.SSHRuleTags(), cfg.Admin.Username, ts.RootPolicy, ts.AutomationSource(), ts.HumanSSHAction())
	if err != nil {
		return change, nil, err
	}
	if err := recordSSHPolicyChange(stPath, cfg, change); err != nil {
		return change, nil, err
	}
	var keptFor []string
	if revoke && ts.AutomationSource() == "" {
		own, err := state.Load(stPath)
		if err != nil {
			return change, nil, err
		}
		keptFor, err = automationRuleSharers(cfg, own.Tailscale.Tailnet)
		if err != nil {
			return change, nil, fmt.Errorf("decide whether to revoke the automation SSH rule for %s/%s: %w", cfg.Namespace, targetServer(cfg.Server), err)
		}
		if len(keptFor) == 0 {
			removed, err := client.RemoveAutomationSSHRule(ctx, ts.SSHRuleTags(), cfg.Admin.Username)
			if err != nil {
				return change, nil, err
			}
			change.AutomationSSHRuleRemoved = removed.AutomationSSHRuleRemoved
		}
	}
	return change, keptFor, client.ValidateSSHPolicy(ctx, ts.Tags, ts.SSHRuleTags(), cfg.Admin.Username, ts.RootPolicy, ts.AutomationSource(), ts.HumanSSHAction())
}

func recordSSHPolicyChange(stPath string, cfg config.Config, change mesh.PolicyChange) error {
	return state.Update(stPath, func(st *state.State) error {
		lifecycle.RecordTailscalePolicyChange(st, cfg, change)
		return nil
	})
}

// automationRuleSharers lists the other registered servers whose state
// records the automation source for the same SSH rule tags and admin user. Those
// servers share the one accept rule, so a revoke for this server would cut
// them off. A server recorded on another tailnet does not share the rule.
// Any unreadable registry, state or config fails closed: the caller then
// keeps the rule and reports the error.
func automationRuleSharers(cfg config.Config, tailnet string) ([]string, error) {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return nil, err
	}
	self := targetServer(cfg.Server)
	var sharers []string
	for _, entry := range reg.List("") {
		if entry.Namespace == cfg.Namespace && entry.Server == self {
			continue
		}
		if entry.StatePath == "" || entry.ConfigPath == "" {
			return nil, fmt.Errorf("registered server %s/%s has no state or config path", entry.Namespace, entry.Server)
		}
		st, err := state.Load(config.Expand(entry.StatePath))
		if err != nil {
			return nil, fmt.Errorf("load registered server %s/%s state: %w", entry.Namespace, entry.Server, err)
		}
		if st.Tailscale.PolicySSHAutomationSource == "" || (tailnet != "" && st.Tailscale.Tailnet != "" && st.Tailscale.Tailnet != tailnet) {
			continue
		}
		other, err := config.Load(config.Expand(entry.ConfigPath))
		if err != nil {
			return nil, fmt.Errorf("load registered server %s/%s config: %w", entry.Namespace, entry.Server, err)
		}
		if other.Admin.Username == cfg.Admin.Username && sameTagSet(other.Access.Tailscale.SSHRuleTags(), cfg.Access.Tailscale.SSHRuleTags()) {
			sharers = append(sharers, entry.Namespace+"/"+entry.Server)
		}
	}
	slices.Sort(sharers)
	return sharers, nil
}

// sameTagSet matches the provider's rule shape, which compares destinations
// as a set.
func sameTagSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
