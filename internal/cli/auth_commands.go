package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/filedescriptor"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// authSecretInputLimit caps piped secret input. Real tokens and recovery keys
// are a few KiB at most; the cap stops a mis-pointed pipe from filling memory.
const authSecretInputLimit = 64 << 10

// authRotateAdvice is printed after a migration because tokens that sat in
// plaintext may already have been copied (backups, sync, other users).
const authRotateAdvice = "these tokens were stored unencrypted before; rotate them at each provider"

// errAuthVerifyFailed makes `auth verify` exit nonzero after it has printed
// the per-file report, so scripts and agents can gate on it.
var errAuthVerifyFailed = errors.New("credential verification failed")

// authStatusRow is the `auth status` view: where the key lives and how many
// credential files are in each state. It never carries a key or a token.
type authStatusRow struct {
	credentials.KeyStatus
	Encrypted  int `json:"encrypted_files"`
	Plaintext  int `json:"plaintext_files"`
	Unreadable int `json:"unreadable_files"`
}

// authResultRow reports one auth mutation in the JSON shape other commands use.
type authResultRow struct {
	Status      string `json:"status"`
	DryRun      bool   `json:"dry_run,omitempty"`
	Store       string `json:"store,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Server      string `json:"server,omitempty"`
	Field       string `json:"field,omitempty"`
	Files       int    `json:"files,omitempty"`
	RecoveryKey string `json:"recovery_key,omitempty"`
	Advice      string `json:"advice,omitempty"`
	// Paths and Reports carry migrate's per-file detail, so its dry run
	// and real run share one schema for agents parsing the output.
	Paths   []string                 `json:"paths,omitempty"`
	Reports []credentials.FileReport `json:"reports,omitempty"`
}

// authCmd groups credential and data-key management. WHY a separate group:
// agents and operators need to inspect, repair, and back up credentials
// without running a provisioning command that happens to touch them.
func (a *app) authCmd() *cobra.Command {
	cmd := parentCommand("auth", "manage stored credentials and the credential encryption key")
	cmd.AddCommand(
		a.authInitCmd(), a.authRecoveryKeyCmd(), a.authStatusCmd(), a.authListCmd(),
		a.authSetCmd(), a.authRemoveCmd(), a.authVerifyCmd(), a.authMigrateCmd(),
		a.authRotateKeyCmd(), a.authRestoreCmd(),
	)
	return cmd
}

func (a *app) authInitCmd() *cobra.Command {
	return &cobra.Command{Use: "init", Short: "create the credential key and print its recovery key once", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		key, created, err := credentials.Init()
		if err != nil {
			return err
		}
		row := authResultRow{Status: "exists", Store: credentials.Status().Store}
		if created {
			row.Status, row.RecoveryKey = "created", key
			row.Advice = "store the recovery key in a password manager; it is the only way to read these credentials if this store is lost"
		}
		return writeJSON(a.stdout, row)
	}}
}

// authRecoveryKeyCmd prints the bare key line so it can be piped straight
// into a password manager CLI or `age -d -i -`.
func (a *app) authRecoveryKeyCmd() *cobra.Command {
	return &cobra.Command{Use: "recovery-key", Short: "print the credential recovery key", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		key, err := credentials.RecoveryKey()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(a.stdout, key)
		return err
	}}
}

func (a *app) authStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "show the key store and credential file states", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		reports, err := credentials.Inventory()
		if err != nil {
			return err
		}
		row := authStatusRow{KeyStatus: credentials.Status()}
		for _, report := range reports {
			switch report.State {
			case credentials.FileEncrypted:
				row.Encrypted++
			case credentials.FilePlaintext:
				row.Plaintext++
			default:
				row.Unreadable++
			}
		}
		return writeJSON(a.stdout, row)
	}}
}

func (a *app) authListCmd() *cobra.Command {
	return withScopedFlags(&cobra.Command{Use: "list", Short: "list stored credential fields (names only, never values)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		reports, err := a.namespaceReports()
		if err != nil {
			return err
		}
		return writeJSON(a.stdout, reports)
	}}, "namespace")
}

func (a *app) authSetCmd() *cobra.Command {
	return withScopedFlags(&cobra.Command{
		Use:   "set SERVER FIELD",
		Short: "store one credential; the value is read from stdin or a masked prompt",
		Long:  "Store one credential for SERVER in the namespace given by -n.\nFIELD is one of: " + strings.Join(credentials.FieldNames(), ", ") + ".\nValues are never accepted as arguments, so they stay out of shell history and process lists.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runAuthSet(args[0], args[1])
		},
	}, "namespace")
}

func (a *app) runAuthSet(server, field string) error {
	cfg, err := a.authTarget(server)
	if err != nil {
		return err
	}
	if err := credentials.ValidateField(field); err != nil {
		return err
	}
	value, err := a.readSecretInput(field)
	if err != nil {
		return err
	}
	if err := credentials.SetField(cfg, field, value); err != nil {
		return err
	}
	return writeJSON(a.stdout, authResultRow{Status: "stored", Namespace: cfg.Namespace, Server: cfg.Server, Field: field})
}

func (a *app) authRemoveCmd() *cobra.Command {
	return withScopedFlags(&cobra.Command{Use: "remove SERVER FIELD", Short: "remove one stored credential", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := a.authTarget(args[0])
		if err != nil {
			return err
		}
		if err := credentials.RemoveField(cfg, args[1]); err != nil {
			return err
		}
		return writeJSON(a.stdout, authResultRow{Status: "removed", Namespace: cfg.Namespace, Server: cfg.Server, Field: args[1]})
	}}, "namespace")
}

// authVerifyCmd proves every credential file opens with the active key and
// is encrypted; it prints the report first so a failure says which file.
func (a *app) authVerifyCmd() *cobra.Command {
	return withScopedFlags(&cobra.Command{Use: "verify", Short: "check every credential file decrypts with the active key", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		reports, err := a.namespaceReports()
		if err != nil {
			return err
		}
		if err := writeJSON(a.stdout, reports); err != nil {
			return err
		}
		for _, report := range reports {
			if report.State != credentials.FileEncrypted {
				return fmt.Errorf("%w: %s is %s (run serverpro auth migrate for plaintext, serverpro auth restore for a missing key)", errAuthVerifyFailed, report.Path, report.State)
			}
		}
		return nil
	}}, "namespace")
}

func (a *app) authMigrateCmd() *cobra.Command {
	return withScopedFlags(&cobra.Command{Use: "migrate", Short: "encrypt every plaintext credential file in place", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if a.dryRun {
			paths, err := credentials.PlaintextFiles()
			if err != nil {
				return err
			}
			return writeJSON(a.stdout, authResultRow{Status: "planned", DryRun: true, Files: len(paths), Paths: paths})
		}
		migrated, err := credentials.Migrate()
		if err != nil {
			return err
		}
		row := authResultRow{Status: "migrated", Files: len(migrated), Store: credentials.Status().Store, Reports: migrated}
		for _, report := range migrated {
			row.Paths = append(row.Paths, report.Path)
		}
		if len(migrated) > 0 {
			row.Advice = authRotateAdvice
		}
		return writeJSON(a.stdout, row)
	}}, "dry-run")
}

func (a *app) authRotateKeyCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{Use: "rotate-key", Short: "re-encrypt every credential file for a new key, optionally in another store", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		count, err := credentials.RotateKey(store)
		if err != nil {
			return err
		}
		return writeJSON(a.stdout, authResultRow{Status: "rotated", Files: count, Store: credentials.Status().Store, Advice: "save the new recovery key: serverpro auth recovery-key"})
	}}
	cmd.Flags().StringVar(&store, "store", "", fmt.Sprintf("move the key to this store (%s or %s)", credentials.KeyStoreKeyring, credentials.KeyStoreFile))
	return cmd
}

func (a *app) authRestoreCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "restore", Short: "install a recovery key read from stdin or a masked prompt", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		key, err := a.readSecretInput("recovery key")
		if err != nil {
			return err
		}
		if err := credentials.Restore(key, force); err != nil {
			return err
		}
		return writeJSON(a.stdout, authResultRow{Status: "restored", Store: credentials.Status().Store})
	}}
	cmd.Flags().BoolVar(&force, "force", false, "replace a different key already in the store")
	return cmd
}

// authTarget validates the namespace and server before they become a path,
// so a crafted name can never point the write outside the config root.
func (a *app) authTarget(server string) (config.Config, error) {
	if a.project == "" {
		return config.Config{}, fmt.Errorf("--namespace is required")
	}
	if err := validateNamespaceName(a.project); err != nil {
		return config.Config{}, err
	}
	if !config.ValidID(server) {
		return config.Config{}, fmt.Errorf("invalid server %q", server)
	}
	return credentials.ServerConfig(a.project, server), nil
}

// namespaceReports narrows the inventory to -n when given.
func (a *app) namespaceReports() ([]credentials.FileReport, error) {
	reports, err := credentials.Inventory()
	if err != nil || a.project == "" {
		return reports, err
	}
	filtered := make([]credentials.FileReport, 0, len(reports))
	for _, report := range reports {
		if report.Namespace == a.project {
			filtered = append(filtered, report)
		}
	}
	return filtered, nil
}

// readSecretInput takes a secret from a masked prompt on a terminal, or from
// the whole of stdin otherwise, so agents can pipe values in without a
// trailing newline and without the value ever reaching argv.
func (a *app) readSecretInput(label string) (string, error) {
	if file, ok := a.stdin.(*os.File); ok {
		if fd, err := filedescriptor.Int(file); err == nil && term.IsTerminal(fd) {
			return a.promptSecret(label)
		}
	}
	body, err := io.ReadAll(io.LimitReader(a.stdin, authSecretInputLimit+1))
	if err != nil {
		return "", fmt.Errorf("read %s from stdin: %w", label, err)
	}
	if len(body) > authSecretInputLimit {
		return "", fmt.Errorf("%s on stdin exceeds %d bytes", label, authSecretInputLimit)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s required on stdin", label)
	}
	return value, nil
}
