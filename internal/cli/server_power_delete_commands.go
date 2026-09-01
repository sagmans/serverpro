package cli

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/redact"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	defaultServerOperationTimeout       = 10 * time.Minute
	deleteExternalCleanupPreflightError = "tracked external cleanup preflight failed before compute deletion"
)

const (
	deleteResourceTailscaleDevice  = "tailscale_device"
	deleteResourceTailscaleAuthKey = "tailscale_auth_key"
	deleteResourceCloudflareTunnel = "cloudflare_tunnel"
)

type serverOperationRow struct {
	Status           string                         `json:"status"`
	Action           string                         `json:"action"`
	DryRun           bool                           `json:"dry_run,omitempty"`
	Namespace        string                         `json:"namespace"`
	Server           string                         `json:"server"`
	Provider         string                         `json:"provider"`
	Power            string                         `json:"power,omitempty"`
	StatePath        string                         `json:"state_path,omitempty"`
	ComputeServer    string                         `json:"compute_server,omitempty"`
	ManagedResources []compute.ManagedResourceRef   `json:"managed_resources,omitempty"`
	ExternalCleanup  []serverDeleteExternalResource `json:"external_cleanup,omitempty"`
}

type serverDeleteExternalResource struct {
	Type string   `json:"type"`
	ID   string   `json:"id,omitempty"`
	Name string   `json:"name,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

// serverDeleteAuthority freezes every input approved before lock acquisition.
type serverDeleteAuthority struct {
	Registry state.RegistryEntry
	Cleanup  serverDeleteCleanup
	Provider compute.Provider
	Account  compute.Account
}

type serverDeleteExecution struct {
	Cleanup  serverDeleteCleanup
	Provider compute.Provider
	Account  compute.Account
}

func (a *app) runServerPower(ctx context.Context, name string, action compute.PowerAction) error {
	_, st, err := a.loadServerReadState(name)
	if err != nil {
		return err
	}
	if a.dryRun {
		row := serverOperationRow{Status: "planned", Action: string(action), DryRun: true, Namespace: st.Namespace, Server: st.Server, Provider: st.Compute.Provider}
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
	row := serverOperationRow{Status: "complete", Action: string(action), Namespace: st.Namespace, Server: st.Server, Provider: st.Compute.Provider}
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
		externalCleanup, err := serverDeleteExternalCleanupPreview(st)
		if err != nil {
			return err
		}
		plan := serverOperationRow{Status: "planned", Action: "delete", DryRun: true, Namespace: st.Namespace, Server: st.Server, Provider: st.Compute.Provider, StatePath: config.Expand(stPath), ComputeServer: st.Compute.ID, ManagedResources: append([]compute.ManagedResourceRef(nil), st.Compute.ManagedResources...), ExternalCleanup: externalCleanup}
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
	row := serverOperationRow{Status: "complete", Action: "delete", Namespace: st.Namespace, Server: st.Server, Provider: st.Compute.Provider}
	return writeJSON(a.stdout, row)
}

// deleteServerDestructive performs one confirmed delete while preventing a
// namespace-wide deletion from removing its local authority mid-workflow.
func (a *app) deleteServerDestructive(ctx context.Context, name, stPath string, st state.State) error {
	authority, err := a.approveServerDelete(name, stPath, st)
	if err != nil {
		return err
	}
	unlock, err := state.LockServerWorkflow(ctx, config.RegistryPath(), st.NamespaceName(), config.Expand(stPath))
	if err != nil {
		return err
	}
	defer unlock()
	return a.executeApprovedServerDelete(ctx, name, stPath, authority)
}

// deleteServerDestructiveInNamespace acquires only the server lock because its
// namespace-delete caller already owns the exclusive namespace lock.
func (a *app) deleteServerDestructiveInNamespace(ctx context.Context, name, stPath string, st state.State) error {
	authority, err := a.approveServerDelete(name, stPath, st)
	if err != nil {
		return err
	}
	unlock, err := state.LockServerOperation(ctx, config.Expand(stPath))
	if err != nil {
		return err
	}
	defer unlock()
	return a.executeApprovedServerDelete(ctx, name, stPath, authority)
}

func (a *app) executeApprovedServerDelete(ctx context.Context, name, stPath string, authority serverDeleteAuthority) error {
	execution, err := a.revalidateServerDelete(name, stPath, authority)
	if err != nil {
		return err
	}
	return a.executeServerDelete(ctx, authority, execution)
}

func (a *app) approveServerDelete(name, stPath string, st state.State) (serverDeleteAuthority, error) {
	registry, exists, err := serverDeleteRegistryAuthority(st)
	if err != nil {
		return serverDeleteAuthority{}, err
	}
	if !serverDeleteRegistryTargetsState(registry, exists, stPath, st) {
		return serverDeleteAuthority{}, fmt.Errorf("server destructive authority changed before delete; rerun delete to review current resources")
	}
	cleanup, err := a.prepareServerDeleteCleanup(name, stPath, st)
	if err != nil {
		return serverDeleteAuthority{}, err
	}
	provider, account, err := a.serverProviderAccount(st)
	if err != nil {
		return serverDeleteAuthority{}, err
	}
	return serverDeleteAuthority{Registry: registry, Cleanup: cleanup, Provider: provider, Account: account}, nil
}

func (a *app) revalidateServerDelete(name, stPath string, authority serverDeleteAuthority) (serverDeleteExecution, error) {
	current, err := state.Load(config.Expand(stPath))
	if err != nil {
		return serverDeleteExecution{}, err
	}
	if !sameServerDeleteAuthority(authority.Cleanup.State, current) {
		return serverDeleteExecution{}, serverDeleteAuthorityChangedError()
	}
	currentRegistry, exists, err := serverDeleteRegistryAuthority(current)
	if err != nil {
		return serverDeleteExecution{}, err
	}
	if !sameServerDeleteRegistryAuthority(authority.Registry, true, currentRegistry, exists) {
		return serverDeleteExecution{}, serverDeleteAuthorityChangedError()
	}
	cleanup, err := a.prepareServerDeleteCleanup(name, stPath, current)
	if err != nil {
		return serverDeleteExecution{}, err
	}
	if !sameServerDeleteCleanupAuthority(authority.Cleanup, cleanup) {
		return serverDeleteExecution{}, serverDeleteAuthorityChangedError()
	}
	provider, account, err := a.serverProviderAccount(current)
	if err != nil {
		return serverDeleteExecution{}, err
	}
	if authority.Provider.Name() != provider.Name() || !reflect.DeepEqual(authority.Account, account) {
		return serverDeleteExecution{}, serverDeleteAuthorityChangedError()
	}
	return serverDeleteExecution{Cleanup: cleanup, Provider: provider, Account: account}, nil
}

func (a *app) executeServerDelete(ctx context.Context, authority serverDeleteAuthority, execution serverDeleteExecution) error {
	st := execution.Cleanup.State
	operationCtx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	if execution.Cleanup.Required {
		if err := a.preflightTrackedExternalResources(operationCtx, execution.Cleanup); err != nil {
			safeErr := redact.New(a.redactionSecrets(execution.Cleanup.Creds)...).Error(err)
			return fmt.Errorf("%s: %w", deleteExternalCleanupPreflightError, safeErr)
		}
	}
	diagnostics := execution.Provider.Delete(operationCtx, compute.DeleteServerRequest{Account: execution.Account, Record: serverRecordFromState(st)})
	if !diagnostics.Passed() {
		return diagnostics.Err()
	}
	if execution.Cleanup.Required {
		if _, err := a.deleteTrackedExternalResources(ctx, execution.Cleanup); err != nil {
			return err
		}
	}
	if err := state.RemoveDurably(config.Expand(execution.Cleanup.StatePath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Delete registry metadata with state so future reads never chase stale files.
	return state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		currentRegistry, exists := reg.Find(st.Namespace, targetServer(st.Server))
		if !sameServerDeleteRegistryAuthority(authority.Registry, true, currentRegistry, exists) {
			return fmt.Errorf("server destructive authority changed before registry cleanup; retained current registry entry")
		}
		reg.Remove(st.Namespace, targetServer(st.Server))
		return nil
	})
}

func serverDeleteAuthorityChangedError() error {
	return fmt.Errorf("server destructive authority changed while awaiting delete lock; rerun delete to review current resources")
}

func sameServerDeleteAuthority(approved, current state.State) bool {
	return approved.Namespace == current.Namespace &&
		approved.Server == current.Server &&
		approved.Compute.Provider == current.Compute.Provider &&
		approved.Compute.ID == current.Compute.ID &&
		approved.Compute.Name == current.Compute.Name &&
		reflect.DeepEqual(approved.Compute.ManagedResources, current.Compute.ManagedResources) &&
		reflect.DeepEqual(approved.Compute.ProviderState, current.Compute.ProviderState) &&
		approved.Tailscale.Tailnet == current.Tailscale.Tailnet &&
		approved.Tailscale.NodeID == current.Tailscale.NodeID &&
		approved.Tailscale.AuthKeyID == current.Tailscale.AuthKeyID &&
		approved.Tailscale.Name == current.Tailscale.Name &&
		reflect.DeepEqual(approved.Tailscale.Tags, current.Tailscale.Tags) &&
		approved.Cloudflare.TunnelID == current.Cloudflare.TunnelID &&
		approved.Cloudflare.Name == current.Cloudflare.Name &&
		approved.Cloudflare.Provenance == current.Cloudflare.Provenance
}

func serverDeleteRegistryAuthority(st state.State) (state.RegistryEntry, bool, error) {
	registry, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return state.RegistryEntry{}, false, err
	}
	entry, exists := registry.Find(st.Namespace, targetServer(st.Server))
	return entry, exists, nil
}

func serverDeleteRegistryTargetsState(entry state.RegistryEntry, exists bool, stPath string, st state.State) bool {
	if !exists {
		return false
	}
	if entry.Namespace != st.Namespace || targetServer(entry.Server) != targetServer(st.Server) || config.Expand(entry.StatePath) != config.Expand(stPath) {
		return false
	}
	if st.Compute.Name != "" && entry.ResourceNames.ComputeServer != "" && entry.ResourceNames.ComputeServer != st.Compute.Name {
		return false
	}
	return st.Cloudflare.Name == "" || entry.ResourceNames.CloudflareTunnel == "" || entry.ResourceNames.CloudflareTunnel == st.Cloudflare.Name
}

func sameServerDeleteRegistryAuthority(approved state.RegistryEntry, approvedExists bool, current state.RegistryEntry, currentExists bool) bool {
	if approvedExists != currentExists {
		return false
	}
	if !approvedExists {
		return true
	}
	approved.StatePath = config.Expand(approved.StatePath)
	approved.ConfigPath = config.Expand(approved.ConfigPath)
	approved.CredentialsPath = config.Expand(approved.CredentialsPath)
	// Registry timestamps are bookkeeping, not destructive authority.
	approved.CreatedAt = time.Time{}
	approved.UpdatedAt = time.Time{}
	current.StatePath = config.Expand(current.StatePath)
	current.ConfigPath = config.Expand(current.ConfigPath)
	current.CredentialsPath = config.Expand(current.CredentialsPath)
	current.CreatedAt = time.Time{}
	current.UpdatedAt = time.Time{}
	return reflect.DeepEqual(approved, current)
}

func sameServerDeleteCleanupAuthority(approved, current serverDeleteCleanup) bool {
	return approved.Required == current.Required &&
		config.Expand(approved.StatePath) == config.Expand(current.StatePath) &&
		sameServerDeleteAuthority(approved.State, current.State) &&
		reflect.DeepEqual(approved.Config, current.Config) &&
		reflect.DeepEqual(approved.Creds, current.Creds)
}

func serverDeleteConfirmMessage(st state.State) string {
	if serverDeleteCleanupRequired(st) {
		return "delete managed server, local state, and tracked external provider resources?"
	}
	return "delete managed server and local state?"
}

func serverDeleteExternalCleanupPreview(st state.State) ([]serverDeleteExternalResource, error) {
	var resources []serverDeleteExternalResource
	if st.Tailscale.NodeID != "" {
		resources = append(resources, serverDeleteExternalResource{Type: deleteResourceTailscaleDevice, ID: st.Tailscale.NodeID, Name: st.Tailscale.Name})
	}
	if st.Tailscale.AuthKeyID != "" {
		resources = append(resources, serverDeleteExternalResource{Type: deleteResourceTailscaleAuthKey, ID: st.Tailscale.AuthKeyID})
	}
	if st.Cloudflare.OwnsTunnel() {
		resources = append(resources, serverDeleteExternalResource{Type: deleteResourceCloudflareTunnel, ID: st.Cloudflare.TunnelID, Name: st.Cloudflare.Name})
	}
	return resources, nil
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
	return provider, compute.Account{Name: st.Namespace + "/" + st.Server, Provider: provider.Name(), Token: creds.ServerProvider, Scope: st.Namespace + "/" + st.Server}, nil
}

func serverCredentialConfig(st state.State) config.Config {
	cfg := config.ExampleServer(st.Namespace, st.Server)
	cfg.Credentials.JSONPath = config.ServerCredentialsPath(st.Namespace, st.Server)
	return cfg
}
