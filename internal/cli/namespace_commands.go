package cli

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
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
		return a.runNamespaceCreate(cmd.Context(), args[0])
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

func (a *app) runNamespaceCreate(ctx context.Context, namespace string) error {
	if err := validateNamespaceName(namespace); err != nil {
		return err
	}
	cfgDir := config.NamespaceConfigDir(namespace)
	stDir := config.NamespaceStateDir(namespace)
	if a.dryRun {
		return writeJSON(a.stdout, namespaceRow{Status: "planned", DryRun: true, Namespace: namespace, ConfigPath: cfgDir, StatePath: stDir})
	}
	unlockNamespace, err := state.LockNamespaceOperationExclusive(ctx, config.RegistryPath(), namespace)
	if err != nil {
		return err
	}
	defer unlockNamespace()
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
