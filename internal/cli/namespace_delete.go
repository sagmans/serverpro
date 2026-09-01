package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/spf13/cobra"
)

const (
	namespaceServersDirName  = "servers"
	namespaceConfigSuffix    = ".yaml"
	namespaceStateSuffix     = ".json"
	namespaceImportSuffix    = ".import.json"
	namespaceCredentialsName = "credentials.json"
)

type namespaceDeleteRow struct {
	Status       string                      `json:"status"`
	DryRun       bool                        `json:"dry_run,omitempty"`
	Namespace    string                      `json:"namespace"`
	ServerCount  int                         `json:"server_count"`
	Servers      []namespaceDeleteServerPlan `json:"servers,omitempty"`
	LocalCleanup []string                    `json:"local_cleanup,omitempty"`
}

type namespaceDeleteServerPlan struct {
	Namespace       string                         `json:"namespace"`
	Server          string                         `json:"server"`
	Provider        string                         `json:"provider,omitempty"`
	ComputeServer   string                         `json:"compute_server,omitempty"`
	StatePath       string                         `json:"state_path,omitempty"`
	ConfigPath      string                         `json:"config_path,omitempty"`
	CredentialsPath string                         `json:"credentials_path,omitempty"`
	ExternalCleanup []serverDeleteExternalResource `json:"external_cleanup,omitempty"`
}

type namespaceDeleteServerAuthority struct {
	Entry       state.RegistryEntry
	State       state.State
	StateExists bool
}

// namespaceDeleteAuthority freezes registry and state evidence before approval.
type namespaceDeleteAuthority struct {
	namespace string
	entries   []state.RegistryEntry
	servers   []namespaceDeleteServerAuthority
}

func (a *app) namespaceDeleteCmd() *cobra.Command {
	return withScopedFlags(&cobra.Command{Use: "delete NAME", Short: "delete a namespace and its managed servers", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runNamespaceDelete(cmd.Context(), args[0])
	}}, "dry-run", "non-interactive", "yes")
}

func (a *app) runNamespaceDelete(ctx context.Context, namespace string) error {
	if err := validateNamespaceName(namespace); err != nil {
		return err
	}
	authority, err := loadNamespaceDeleteAuthority(namespace)
	if err != nil {
		return err
	}
	done, err := a.previewNamespaceDelete(authority)
	if err != nil || done {
		return err
	}
	if err := a.confirmNamespaceDelete(authority); err != nil {
		return err
	}
	unlock, err := state.LockNamespaceOperationExclusive(ctx, config.RegistryPath(), namespace)
	if err != nil {
		return err
	}
	defer unlock()
	if err := revalidateNamespaceDeleteAuthority(authority); err != nil {
		return err
	}
	return a.executeNamespaceDelete(ctx, authority)
}

func loadNamespaceDeleteAuthority(namespace string) (namespaceDeleteAuthority, error) {
	registry, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return namespaceDeleteAuthority{}, err
	}
	if !namespaceExists(registry, namespace) {
		return namespaceDeleteAuthority{}, fmt.Errorf("namespace %q not found", namespace)
	}
	entries := registry.List(namespace)
	if err := validateNamespaceLocalAuthority(namespace, entries); err != nil {
		return namespaceDeleteAuthority{}, err
	}
	servers, err := loadNamespaceDeleteServerAuthorities(entries)
	if err != nil {
		return namespaceDeleteAuthority{}, err
	}
	return namespaceDeleteAuthority{namespace: namespace, entries: entries, servers: servers}, nil
}

func validateNamespaceLocalAuthority(namespace string, entries []state.RegistryEntry) error {
	allowed := registeredNamespaceAuthority(entries)
	if err := validateNamespaceStateAuthority(namespace, allowed); err != nil {
		return err
	}
	return validateNamespaceConfigAuthority(namespace, allowed)
}

func registeredNamespaceAuthority(entries []state.RegistryEntry) map[string]bool {
	allowed := make(map[string]bool)
	for _, entry := range entries {
		for _, path := range []string{entry.StatePath, entry.ConfigPath, entry.CredentialsPath} {
			if path != "" {
				allowed[filepath.Clean(config.Expand(path))] = true
			}
		}
		if entry.StatePath != "" {
			allowed[filepath.Clean(config.Expand(entry.StatePath)+namespaceImportSuffix)] = true
		}
	}
	return allowed
}

func validateNamespaceStateAuthority(namespace string, allowed map[string]bool) error {
	stateDir := filepath.Join(config.NamespaceStateDir(namespace), namespaceServersDirName)
	entries, err := os.ReadDir(stateDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), namespaceStateSuffix) {
			continue
		}
		path := filepath.Join(stateDir, entry.Name())
		if !allowed[filepath.Clean(path)] {
			return fmt.Errorf("namespace %q has unregistered local authority at %s", namespace, path)
		}
	}
	return nil
}

func validateNamespaceConfigAuthority(namespace string, allowed map[string]bool) error {
	configDir := filepath.Join(config.NamespaceConfigDir(namespace), namespaceServersDirName)
	entries, err := os.ReadDir(configDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(configDir, entry.Name())
		if !entry.IsDir() {
			if strings.HasSuffix(entry.Name(), namespaceConfigSuffix) && !allowed[filepath.Clean(path)] {
				return fmt.Errorf("namespace %q has unregistered local authority at %s", namespace, path)
			}
			continue
		}
		credentialsPath := filepath.Join(path, namespaceCredentialsName)
		if _, err := os.Lstat(credentialsPath); err == nil {
			if !allowed[filepath.Clean(credentialsPath)] {
				return fmt.Errorf("namespace %q has unregistered local authority at %s", namespace, credentialsPath)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (a *app) previewNamespaceDelete(authority namespaceDeleteAuthority) (bool, error) {
	if !a.dryRun && (a.yes || a.nonInteractive) {
		return false, nil
	}
	plan, err := a.buildNamespaceDeletePlan(authority.namespace, authority.servers)
	if err != nil {
		return false, err
	}
	if err := writeJSON(a.stdout, plan); err != nil {
		return false, err
	}
	return a.dryRun, nil
}

func (a *app) confirmNamespaceDelete(authority namespaceDeleteAuthority) error {
	if a.yes {
		return nil
	}
	if a.nonInteractive {
		return fmt.Errorf("--yes required for non-interactive namespace delete")
	}
	return a.confirm(namespaceDeleteConfirmMessage(authority.namespace, len(authority.entries)))
}

func revalidateNamespaceDeleteAuthority(authority namespaceDeleteAuthority) error {
	registry, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return err
	}
	currentEntries := registry.List(authority.namespace)
	if !namespaceExists(registry, authority.namespace) || !sameNamespaceDeleteRegistryAuthority(authority.entries, currentEntries) {
		return namespaceDeleteAuthorityChangedError()
	}
	if err := validateNamespaceLocalAuthority(authority.namespace, currentEntries); err != nil {
		return err
	}
	for _, approved := range authority.servers {
		current, err := state.Load(config.Expand(approved.Entry.StatePath))
		currentExists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if currentExists != approved.StateExists || (currentExists && !reflect.DeepEqual(approved.State, current)) {
			return namespaceDeleteAuthorityChangedError()
		}
	}
	return nil
}

func (a *app) executeNamespaceDelete(ctx context.Context, authority namespaceDeleteAuthority) error {
	a.project = authority.namespace
	for _, approved := range authority.servers {
		entry := approved.Entry
		if !approved.StateExists {
			// Missing state cannot safely reconstruct provider deletion; only the
			// stale registry entry may be removed.
			if err := removeNamespaceServerRegistryEntry(entry); err != nil {
				return err
			}
			continue
		}
		if err := a.deleteServerDestructiveInNamespace(ctx, entry.Server, entry.StatePath, approved.State); err != nil {
			return err
		}
	}
	if err := removeNamespaceLocalState(authority.namespace); err != nil {
		return err
	}
	if err := removeEmptyNamespaceRegistry(authority.namespace); err != nil {
		return err
	}
	return writeJSON(a.stdout, namespaceDeleteRow{Status: "complete", Namespace: authority.namespace, ServerCount: len(authority.servers)})
}

func removeNamespaceLocalState(namespace string) error {
	for _, path := range []string{config.NamespaceConfigDir(namespace), config.NamespaceStateDir(namespace)} {
		if err := os.RemoveAll(config.Expand(path)); err != nil {
			return err
		}
	}
	return nil
}

func removeEmptyNamespaceRegistry(namespace string) error {
	return state.UpdateRegistry(config.RegistryPath(), func(registry *state.Registry) error {
		if len(registry.List(namespace)) != 0 {
			return fmt.Errorf("namespace destructive authority changed before registry cleanup; retained current namespace")
		}
		registry.RemoveNamespace(namespace)
		return nil
	})
}

func namespaceDeleteAuthorityChangedError() error {
	return fmt.Errorf("namespace destructive authority changed while awaiting delete lock; rerun delete to review current resources")
}

func loadNamespaceDeleteServerAuthorities(servers []state.RegistryEntry) ([]namespaceDeleteServerAuthority, error) {
	authorities := make([]namespaceDeleteServerAuthority, 0, len(servers))
	for _, entry := range servers {
		st, err := state.Load(config.Expand(entry.StatePath))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		authorities = append(authorities, namespaceDeleteServerAuthority{Entry: entry, State: st, StateExists: err == nil})
	}
	return authorities, nil
}

func (a *app) buildNamespaceDeletePlan(namespace string, authorities []namespaceDeleteServerAuthority) (namespaceDeleteRow, error) {
	row := namespaceDeleteRow{Status: "planned", DryRun: true, Namespace: namespace, ServerCount: len(authorities)}
	for _, authority := range authorities {
		entry := authority.Entry
		if !authority.StateExists {
			// Keep stale entries visible in previews so users see which local
			// metadata will be purged.
			row.Servers = append(row.Servers, staleNamespaceDeleteServerPlan(entry))
			continue
		}
		st := authority.State
		externalCleanup, err := serverDeleteExternalCleanupPreview(st)
		if err != nil {
			return row, err
		}
		row.Servers = append(row.Servers, namespaceDeleteServerPlan{
			Namespace:       entry.Namespace,
			Server:          entry.Server,
			Provider:        st.Compute.Provider,
			ComputeServer:   st.Compute.Name,
			StatePath:       config.Expand(entry.StatePath),
			ConfigPath:      config.Expand(entry.ConfigPath),
			CredentialsPath: config.Expand(entry.CredentialsPath),
			ExternalCleanup: externalCleanup,
		})
	}
	row.LocalCleanup = []string{
		config.Expand(config.NamespaceConfigDir(namespace)),
		config.Expand(config.NamespaceStateDir(namespace)),
	}
	return row, nil
}

func staleNamespaceDeleteServerPlan(entry state.RegistryEntry) namespaceDeleteServerPlan {
	return namespaceDeleteServerPlan{
		Namespace:       entry.Namespace,
		Server:          entry.Server,
		StatePath:       config.Expand(entry.StatePath),
		ConfigPath:      config.Expand(entry.ConfigPath),
		CredentialsPath: config.Expand(entry.CredentialsPath),
	}
}

func removeNamespaceServerRegistryEntry(entry state.RegistryEntry) error {
	return state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		current, exists := reg.Find(entry.Namespace, entry.Server)
		if !exists {
			return nil
		}
		if !sameServerDeleteRegistryAuthority(entry, true, current, true) {
			return fmt.Errorf("namespace destructive authority changed before stale registry cleanup; retained current entry")
		}
		reg.Remove(entry.Namespace, entry.Server)
		return nil
	})
}

func sameNamespaceDeleteRegistryAuthority(approved, current []state.RegistryEntry) bool {
	if len(approved) != len(current) {
		return false
	}
	for i := range approved {
		if !sameServerDeleteRegistryAuthority(approved[i], true, current[i], true) {
			return false
		}
	}
	return true
}

func namespaceDeleteConfirmMessage(namespace string, count int) string {
	if count == 0 {
		return fmt.Sprintf("delete namespace %q and its local state?", namespace)
	}
	return fmt.Sprintf("delete namespace %q and its %d managed server(s), local state, and tracked external provider resources?", namespace, count)
}
