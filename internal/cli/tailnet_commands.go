package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/spf13/cobra"
)

type tailnetPolicyClient interface {
	PlanServerproPolicyReconcile(context.Context, []string) (mesh.PolicyReconcilePlan, error)
	ApplyServerproPolicyReconcile(context.Context, []string, mesh.PolicyReconcilePlan) error
}

type tailnetReconcileRow struct {
	Status    string         `json:"status"`
	DryRun    bool           `json:"dry_run,omitempty"`
	Tailnet   string         `json:"tailnet"`
	TagOwners []string       `json:"tag_owners"`
	SSHRules  []mesh.SSHRule `json:"ssh_rules"`
}

func (a *app) tailnetCmd() *cobra.Command {
	cmd := parentCommand("tailnet", "manage shared tailnet policy")
	cmd.AddCommand(a.tailnetReconcileCmd())
	return cmd
}

func (a *app) tailnetReconcileCmd() *cobra.Command {
	tailnet := ""
	cmd := &cobra.Command{Use: "reconcile", Short: "remove unused serverpro tailnet policy", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return a.runTailnetReconcile(cmd.Context(), tailnet)
	}}
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "explicit Tailscale tailnet identity")
	return cmd
}

func (a *app) runTailnetReconcile(ctx context.Context, tailnet string) error {
	if tailnet == "" || tailnet == config.TokenDefaultTailnet {
		return fmt.Errorf("--tailnet requires an explicit tailnet identity")
	}
	token, err := a.tailnetToken()
	if err != nil {
		return err
	}
	operationCtx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	unlock, err := state.LockTailnetPolicy(operationCtx, config.RegistryPath(), tailnet)
	if err != nil {
		return err
	}
	defer unlock()
	protectedTags, err := registeredTailnetTags(tailnet)
	if err != nil {
		return err
	}
	client := a.tailnetPolicyClient(token, tailnet)
	plan, err := client.PlanServerproPolicyReconcile(operationCtx, protectedTags)
	if err != nil {
		return err
	}
	planned := tailnetReconcileRow{Status: "planned", DryRun: true, Tailnet: tailnet, TagOwners: plan.TagOwners, SSHRules: plan.SSHRules}
	if a.dryRun {
		return writeJSON(a.stdout, planned)
	}
	if plan.Empty() {
		planned.Status = "complete"
		planned.DryRun = false
		return writeJSON(a.stdout, planned)
	}
	if !a.yes {
		if a.nonInteractive {
			return fmt.Errorf("--yes required for non-interactive tailnet reconcile")
		}
		previewWriter := a.stderr
		if previewWriter == nil {
			previewWriter = io.Discard
		}
		if err := writeJSON(previewWriter, planned); err != nil {
			return err
		}
		if err := a.confirm("remove tailnet policy entries with no registered or live device evidence?"); err != nil {
			return err
		}
	}
	if err := client.ApplyServerproPolicyReconcile(operationCtx, protectedTags, plan); err != nil {
		return err
	}
	return writeJSON(a.stdout, tailnetReconcileRow{Status: "complete", Tailnet: tailnet, TagOwners: plan.TagOwners, SSHRules: plan.SSHRules})
}

func registeredTailnetTags(tailnet string) ([]string, error) {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, entry := range reg.List("") {
		if entry.StatePath == "" {
			return nil, fmt.Errorf("registered server %s/%s has no state path", entry.Namespace, entry.Server)
		}
		st, err := state.Load(config.Expand(entry.StatePath))
		if err != nil {
			return nil, fmt.Errorf("load registered server %s/%s state: %w", entry.Namespace, entry.Server, err)
		}
		tags := stateTailnetPolicyTags(st.Tailscale)
		if len(tags) == 0 {
			continue
		}
		if st.Tailscale.Tailnet == "" || st.Tailscale.Tailnet == config.TokenDefaultTailnet {
			return nil, fmt.Errorf("registered server %s/%s has no stable tailnet identity", entry.Namespace, entry.Server)
		}
		if st.Tailscale.Tailnet != tailnet {
			continue
		}
		for _, tag := range tags {
			set[tag] = true
		}
	}
	tags := make([]string, 0, len(set))
	for tag := range set {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}

func stateTailnetPolicyTags(st state.TailscaleState) []string {
	tags := append([]string{}, st.Tags...)
	tags = append(tags, st.PolicyTagOwners...)
	return append(tags, st.PolicySSHTags...)
}

func (a *app) tailnetToken() (string, error) {
	token := os.Getenv("SERVERPRO_TAILSCALE_TOKEN")
	if token == "" {
		token = os.Getenv("TAILSCALE_API_TOKEN")
	}
	if token == "" {
		if a.nonInteractive {
			return "", fmt.Errorf("SERVERPRO_TAILSCALE_TOKEN required for non-interactive tailnet reconcile")
		}
		var err error
		token, err = a.promptSecret("Tailscale API token")
		if err != nil {
			return "", err
		}
	}
	a.addRuntimeSecret(token)
	return token, nil
}
