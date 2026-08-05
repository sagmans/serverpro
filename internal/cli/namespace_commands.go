package cli

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
	"github.com/spf13/cobra"
)

type namespaceRow struct {
	Status      string `json:"status,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
	Namespace   string `json:"namespace"`
	ServerCount int    `json:"server_count,omitempty"`
	ConfigPath  string `json:"config_path,omitempty"`
	StatePath   string `json:"state_path,omitempty"`
}

func (a *app) namespaceCmd() *cobra.Command {
	cmd := parentCommand("namespace", "manage serverpro namespaces")
	cmd.AddCommand(a.namespaceCreateCmd(), a.namespaceListCmd(), a.namespaceStatusCmd(), a.namespaceDeleteCmd())
	return cmd
}

func (a *app) namespaceCreateCmd() *cobra.Command {
	return &cobra.Command{Use: "create NAME", Short: "create a namespace", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runNamespaceCreate(args[0])
	}}
}

func (a *app) namespaceListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "list namespaces", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.runNamespaceList()
	}}
}

func (a *app) namespaceStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status NAME", Short: "show namespace status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runNamespaceStatus(args[0])
	}}
}

func (a *app) namespaceDeleteCmd() *cobra.Command {
	return &cobra.Command{Use: "delete NAME", Short: "delete a namespace and its managed servers", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runNamespaceDelete(cmd.Context(), args[0])
	}}
}

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

func (a *app) runNamespaceDelete(ctx context.Context, namespace string) error {
	if err := validateNamespaceName(namespace); err != nil {
		return err
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return err
	}
	if !namespaceExists(reg, namespace) {
		return fmt.Errorf("namespace %q not found", namespace)
	}
	servers := reg.List(namespace)
	// Destructive commands always preview the plan and request approval unless
	// -y is provided. An explicit --dry-run exits after the preview.
	if a.dryRun || (!a.yes && !a.nonInteractive) {
		plan, err := a.buildNamespaceDeletePlan(namespace, servers)
		if err != nil {
			return err
		}
		if a.dryRun {
			return writeJSON(a.stdout, plan)
		}
		if err := writeJSON(a.stdout, plan); err != nil {
			return err
		}
	}
	if !a.yes {
		if a.nonInteractive {
			return fmt.Errorf("--yes required for non-interactive namespace delete")
		}
		if err := a.confirm(namespaceDeleteConfirmMessage(namespace, len(servers))); err != nil {
			return err
		}
	}
	a.project = namespace
	for _, entry := range servers {
		st, err := state.Load(config.Expand(entry.StatePath))
		if err != nil {
			if os.IsNotExist(err) {
				// Stale registry entries should not block cleanup; missing state
				// means provider deletion cannot be reconstructed safely.
				if err := removeNamespaceServerRegistryEntry(entry); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if err := a.deleteServerDestructive(ctx, entry.Server, entry.StatePath, st); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(config.Expand(config.NamespaceConfigDir(namespace))); err != nil {
		return err
	}
	if err := os.RemoveAll(config.Expand(config.NamespaceStateDir(namespace))); err != nil {
		return err
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.RemoveProject(namespace)
		return nil
	}); err != nil {
		return err
	}
	return writeJSON(a.stdout, namespaceDeleteRow{Status: "complete", Namespace: namespace, ServerCount: len(servers)})
}

func (a *app) buildNamespaceDeletePlan(namespace string, servers []state.RegistryEntry) (namespaceDeleteRow, error) {
	row := namespaceDeleteRow{Status: "planned", DryRun: true, Namespace: namespace, ServerCount: len(servers)}
	for _, entry := range servers {
		st, err := state.Load(config.Expand(entry.StatePath))
		if err != nil {
			if os.IsNotExist(err) {
				// Keep stale entries visible in previews so users see which local
				// metadata will be purged.
				row.Servers = append(row.Servers, staleNamespaceDeleteServerPlan(entry))
				continue
			}
			return row, err
		}
		externalCleanup, err := serverDeleteExternalCleanupPreview(entry.StatePath, st)
		if err != nil {
			return row, err
		}
		row.Servers = append(row.Servers, namespaceDeleteServerPlan{
			Namespace:       entry.Project,
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
		Namespace:       entry.Project,
		Server:          entry.Server,
		StatePath:       config.Expand(entry.StatePath),
		ConfigPath:      config.Expand(entry.ConfigPath),
		CredentialsPath: config.Expand(entry.CredentialsPath),
	}
}

func removeNamespaceServerRegistryEntry(entry state.RegistryEntry) error {
	return state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Remove(entry.Project, entry.Server)
		return nil
	})
}

func namespaceDeleteConfirmMessage(namespace string, count int) string {
	if count == 0 {
		return fmt.Sprintf("delete namespace %q and its local state?", namespace)
	}
	return fmt.Sprintf("delete namespace %q and its %d managed server(s), local state, and tracked external provider resources?", namespace, count)
}

func (a *app) runNamespaceCreate(namespace string) error {
	if err := validateNamespaceName(namespace); err != nil {
		return err
	}
	cfgDir := config.NamespaceConfigDir(namespace)
	stDir := config.NamespaceStateDir(namespace)
	if a.dryRun {
		return writeJSON(a.stdout, namespaceRow{Status: "planned", DryRun: true, Namespace: namespace, ConfigPath: cfgDir, StatePath: stDir})
	}
	for _, dir := range []string{cfgDir, stDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.UpsertNamespace(namespace)
		return nil
	}); err != nil {
		return err
	}
	return writeJSON(a.stdout, namespaceRow{Status: "created", Namespace: namespace, ConfigPath: cfgDir, StatePath: stDir})
}

func (a *app) runNamespaceList() error {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return err
	}
	rows := make([]namespaceRow, 0, len(reg.ListNamespaces()))
	for _, namespace := range reg.ListNamespaces() {
		rows = append(rows, namespaceRow{Namespace: namespace, ServerCount: len(reg.List(namespace)), ConfigPath: config.NamespaceConfigDir(namespace), StatePath: config.NamespaceStateDir(namespace)})
	}
	return writeJSON(a.stdout, rows)
}

func (a *app) runNamespaceStatus(namespace string) error {
	if err := validateNamespaceName(namespace); err != nil {
		return err
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return err
	}
	if !namespaceExists(reg, namespace) {
		return fmt.Errorf("namespace %q not found", namespace)
	}
	return writeJSON(a.stdout, namespaceRow{Namespace: namespace, ServerCount: len(reg.List(namespace)), ConfigPath: config.NamespaceConfigDir(namespace), StatePath: config.NamespaceStateDir(namespace)})
}

func namespaceExists(reg state.Registry, namespace string) bool {
	return slices.Contains(reg.ListNamespaces(), namespace)
}

func validateNamespaceName(namespace string) error {
	if !config.ValidID(namespace) {
		return fmt.Errorf("invalid namespace %q", namespace)
	}
	return nil
}
