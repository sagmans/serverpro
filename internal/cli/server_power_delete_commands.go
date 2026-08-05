package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/state"
)

var defaultServerOperationTimeout = 10 * time.Minute

const (
	deleteResourceTailscaleDevice          = "tailscale_device"
	deleteResourceTailscaleAuthKey         = "tailscale_auth_key"
	deleteResourceTailscalePolicyTagOwners = "tailscale_policy_tag_owners"
	deleteResourceTailscaleSSHRule         = "tailscale_ssh_rule"
	deleteResourceCloudflareTunnel         = "cloudflare_tunnel"
	accessPolicyStateKey                   = "access_policy_id"
	firewallStateKey                       = "firewall_id"
	firewallGroupStateKey                  = "firewall_group_id"
)

type serverOperationRow struct {
	Status          string                         `json:"status"`
	Action          string                         `json:"action"`
	DryRun          bool                           `json:"dry_run,omitempty"`
	Namespace       string                         `json:"namespace"`
	Server          string                         `json:"server"`
	Provider        string                         `json:"provider"`
	Power           string                         `json:"power,omitempty"`
	StatePath       string                         `json:"state_path,omitempty"`
	ComputeServer   string                         `json:"compute_server,omitempty"`
	AccessPolicyID  string                         `json:"access_policy_id,omitempty"`
	ExternalCleanup []serverDeleteExternalResource `json:"external_cleanup,omitempty"`
}

type serverDeleteExternalResource struct {
	Type string   `json:"type"`
	ID   string   `json:"id,omitempty"`
	Name string   `json:"name,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

func (a *app) runServerPower(ctx context.Context, name string, action compute.PowerAction) error {
	_, st, err := a.loadServerReadState(name)
	if err != nil {
		return err
	}
	if a.dryRun {
		row := serverOperationRow{Status: "planned", Action: string(action), DryRun: true, Namespace: st.Project, Server: st.Server, Provider: st.Compute.Provider}
		return writeJSON(a.stdout, row)
	}
	if !a.yes {
		if a.nonInteractive {
			return fmt.Errorf("--yes required for non-interactive %s", action)
		}
		if err := a.confirm(fmt.Sprintf("%s managed server?", action)); err != nil {
			return err
		}
	}
	provider, accountRef, err := a.serverProviderAccount(st)
	if err != nil {
		return err
	}
	operationCtx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	status, diagnostics := provider.Power(operationCtx, compute.PowerRequest{Account: accountRef, Record: serverRecordFromState(st), Action: action})
	if !diagnostics.Passed() {
		return diagnostics.Err()
	}
	if status.Power != "" {
		row := serverRowFromState(st)
		row.Power = statusPowerLabel(status.Power)
		return writeJSON(a.stdout, row)
	}
	row := serverOperationRow{Status: "complete", Action: string(action), Namespace: st.Project, Server: st.Server, Provider: st.Compute.Provider}
	return writeJSON(a.stdout, row)
}

func (a *app) runServerDelete(ctx context.Context, name string) error {
	stPath, st, err := a.loadServerReadState(name)
	if err != nil {
		return err
	}
	// Destructive commands always preview the plan and request approval unless
	// -y is provided. An explicit --dry-run exits after the preview.
	if a.dryRun || (!a.yes && !a.nonInteractive) {
		externalCleanup, err := serverDeleteExternalCleanupPreview(stPath, st)
		if err != nil {
			return err
		}
		plan := serverOperationRow{Status: "planned", Action: "delete", DryRun: true, Namespace: st.Project, Server: st.Server, Provider: st.Compute.Provider, StatePath: config.Expand(stPath), ComputeServer: st.Compute.ID, AccessPolicyID: trackedAccessPolicyID(st.Compute.ProviderState), ExternalCleanup: externalCleanup}
		if a.dryRun {
			return writeJSON(a.stdout, plan)
		}
		if err := writeJSON(a.stdout, plan); err != nil {
			return err
		}
	}
	if !a.yes {
		if a.nonInteractive {
			return fmt.Errorf("--yes required for non-interactive delete")
		}
		if err := a.confirm(serverDeleteConfirmMessage(st)); err != nil {
			return err
		}
	}
	if err := a.deleteServerDestructive(ctx, name, stPath, st); err != nil {
		return err
	}
	row := serverOperationRow{Status: "complete", Action: "delete", Namespace: st.Project, Server: st.Server, Provider: st.Compute.Provider}
	return writeJSON(a.stdout, row)
}

// deleteServerDestructive performs the destructive provider + local cleanup for
// one server after confirmation has already been obtained by the caller.
func (a *app) deleteServerDestructive(ctx context.Context, name, stPath string, st state.State) error {
	cleanup, err := a.prepareServerDeleteCleanup(name, stPath, st)
	if err != nil {
		return err
	}
	stPath, st = cleanup.StatePath, cleanup.State
	if st.Compute.ID != "" || len(st.Compute.ProviderState) > 0 {
		provider, accountRef, err := a.serverProviderAccount(st)
		if err != nil {
			return err
		}
		operationCtx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
		defer cancel()
		diagnostics := provider.Delete(operationCtx, compute.DeleteServerRequest{Account: accountRef, Record: serverRecordFromState(st)})
		if !diagnostics.Passed() {
			return diagnostics.Err()
		}
	}
	if cleanup.Required {
		if _, err := a.deleteTrackedExternalResources(ctx, cleanup); err != nil {
			return err
		}
	}
	if err := os.Remove(config.Expand(stPath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Delete registry metadata with state so future list/status calls do not chase stale files.
	return state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Remove(st.Project, targetServer(st.Server))
		return nil
	})
}

func trackedAccessPolicyID(providerState map[string]string) string {
	for _, key := range []string{accessPolicyStateKey, firewallStateKey, firewallGroupStateKey} {
		if providerState[key] != "" {
			return providerState[key]
		}
	}
	return ""
}

func serverDeleteConfirmMessage(st state.State) string {
	if serverDeleteCleanupRequired(st) {
		return "delete managed server, local state, and tracked external provider resources?"
	}
	return "delete managed server and local state?"
}

func serverDeleteExternalCleanupPreview(stPath string, st state.State) ([]serverDeleteExternalResource, error) {
	var resources []serverDeleteExternalResource
	if st.Tailscale.NodeID != "" {
		resources = append(resources, serverDeleteExternalResource{Type: deleteResourceTailscaleDevice, ID: st.Tailscale.NodeID, Name: st.Tailscale.Name})
	}
	if st.Tailscale.AuthKeyID != "" {
		resources = append(resources, serverDeleteExternalResource{Type: deleteResourceTailscaleAuthKey, ID: st.Tailscale.AuthKeyID})
	}
	if len(st.Tailscale.PolicyTagOwners) > 0 || st.Tailscale.PolicySSHRule {
		plan, err := tailscalePolicyRemovalPlan(serverDeleteCleanup{StatePath: stPath, State: st, Config: serverDeletePreviewConfig(st)}, st)
		if err != nil {
			return nil, err
		}
		if len(plan.TagOwners) > 0 {
			resources = append(resources, serverDeleteExternalResource{Type: deleteResourceTailscalePolicyTagOwners, Tags: append([]string{}, plan.TagOwners...)})
		}
		if len(plan.SSHTags) > 0 {
			resources = append(resources, serverDeleteExternalResource{Type: deleteResourceTailscaleSSHRule, Tags: append([]string{}, plan.SSHTags...)})
		}
	}
	if st.Cloudflare.TunnelID != "" {
		resources = append(resources, serverDeleteExternalResource{Type: deleteResourceCloudflareTunnel, ID: st.Cloudflare.TunnelID, Name: st.Cloudflare.Name})
	}
	return resources, nil
}

func serverDeletePreviewConfig(st state.State) config.Config {
	cfg := config.Default()
	cfg.Project = st.Project
	cfg.Server = st.Server
	cfg.Compute.Name = st.Compute.Name
	cfg.Cloudflare.Tunnel.Name = st.Cloudflare.Name
	// Dry-run previews must not force config/credential loading. Prefer tracked
	// state tags, then derive the namespace tag used by default server configs.
	cfg.Access.Tailscale.Tags = append([]string{}, st.Tailscale.Tags...)
	if len(cfg.Access.Tailscale.Tags) == 0 && st.Project != "" {
		cfg.Access.Tailscale.Tags = []string{config.ProjectTailscaleTag(st.Project)}
	}
	return cfg
}

func contextWithDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok || timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (a *app) serverProviderAccount(st state.State) (compute.Provider, compute.Account, error) {
	providerName := a.provider
	if providerName == "" {
		providerName = st.Compute.Provider
	}
	provider, err := a.resolveProvider(providerName)
	if err != nil {
		return nil, compute.Account{}, err
	}
	creds, err := credentials.LoadPartial(serverCredentialConfig(st))
	if err != nil {
		return nil, compute.Account{}, err
	}
	if creds.ServerProvider == "" {
		return nil, compute.Account{}, fmt.Errorf("missing credentials: [server provider API token]")
	}
	return provider, compute.Account{Name: st.Project + "/" + st.Server, Provider: provider.Name(), Token: creds.ServerProvider, Scope: st.Project + "/" + st.Server}, nil
}

func serverCredentialConfig(st state.State) config.Config {
	cfg := config.ExampleServer(st.Project, st.Server)
	cfg.Credentials.JSONPath = config.ServerCredentialsPath(st.Project, st.Server)
	return cfg
}
