package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/redact"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/spf13/cobra"
)

const (
	upgradeOperationAction = "upgrade"
	rebootOperationAction  = "reboot"

	maintenanceStatusPlanned  = "planned"
	maintenanceStatusComplete = "complete"
	maintenanceStatusBlocked  = "blocked"
	maintenanceStatusFailed   = "failed"

	upgradeCommandTimeout = 45 * time.Minute
	rebootCommandTimeout  = 15 * time.Minute

	upgradeConfirmMessage = "upgrade OS packages and reboot managed server if required?"
	rebootConfirmMessage  = "reboot managed server?"

	missingTailscaleMaintenanceError = "tailscale host missing from state; run serverpro server create or server doctor"
	upgradeDoctorFailedError         = "doctor failed"
)

type serverUpgradeFlags struct {
	security    bool
	check       bool
	reboot      bool
	forceReboot bool
	noWait      bool
	fix         bool
}

type serverRebootFlags struct {
	ifRequired bool
	force      bool
	noWait     bool
}

type serverUpgradeRow struct {
	Status                 string                  `json:"status"`
	Action                 string                  `json:"action"`
	DryRun                 bool                    `json:"dry_run,omitempty"`
	Namespace              string                  `json:"namespace"`
	Server                 string                  `json:"server"`
	User                   string                  `json:"user,omitempty"`
	Host                   string                  `json:"host,omitempty"`
	Mode                   lifecycle.UpgradeMode   `json:"mode"`
	RebootPlanned          bool                    `json:"reboot_planned,omitempty"`
	FixPlanned             bool                    `json:"fix_planned,omitempty"`
	PendingPackages        []string                `json:"pending_packages,omitempty"`
	UpgradedPackages       []string                `json:"upgraded_packages,omitempty"`
	RebootRequired         bool                    `json:"reboot_required"`
	RebootRequiredPackages []string                `json:"reboot_required_packages,omitempty"`
	Reboot                 *lifecycle.RebootResult `json:"reboot,omitempty"`
	PreflightGaps          []string                `json:"preflight_gaps,omitempty"`
	Doctor                 *doctor.Summary         `json:"doctor,omitempty"`
}

type serverRebootRow struct {
	Status                 string   `json:"status"`
	Action                 string   `json:"action"`
	DryRun                 bool     `json:"dry_run,omitempty"`
	Namespace              string   `json:"namespace"`
	Server                 string   `json:"server"`
	User                   string   `json:"user,omitempty"`
	Host                   string   `json:"host,omitempty"`
	IfRequired             bool     `json:"if_required,omitempty"`
	Force                  bool     `json:"force,omitempty"`
	NoWait                 bool     `json:"no_wait,omitempty"`
	RebootRequired         bool     `json:"reboot_required"`
	RebootRequiredPackages []string `json:"reboot_required_packages,omitempty"`
	Rebooted               bool     `json:"rebooted"`
	Gated                  bool     `json:"gated"`
	BeforeBootID           string   `json:"before_boot_id,omitempty"`
	AfterBootID            string   `json:"after_boot_id,omitempty"`
	VerifyStatus           string   `json:"verify_status,omitempty"`
	VerifyFindings         []string `json:"verify_findings,omitempty"`
	PreflightGaps          []string `json:"preflight_gaps,omitempty"`
}

func (a *app) serverUpgradeCmd() *cobra.Command {
	var flags serverUpgradeFlags
	cmd := withScopedFlags(&cobra.Command{
		Use:   "upgrade NAME",
		Short: "upgrade OS packages and apply security updates on a managed server",
		Long: "Refresh apt metadata and apply Ubuntu package or security upgrades over Tailscale SSH " +
			"using the server's admin sudo password. Optional flags chain a gated reboot and doctor --fix convergence.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a.server = args[0]
			return a.runServerUpgrade(cmd.Context(), args[0], flags)
		},
	}, "config", "state", "namespace", "provider", "non-interactive", "dry-run", "yes")
	cmd.Flags().BoolVar(&flags.security, "security", false, "apply only Ubuntu security-origin upgrades via unattended-upgrade")
	cmd.Flags().BoolVar(&flags.check, "check", false, "refresh apt metadata and list pending upgrades without installing")
	cmd.Flags().BoolVar(&flags.reboot, "reboot", false, "perform a gated reboot if upgrades set /var/run/reboot-required")
	cmd.Flags().BoolVar(&flags.forceReboot, "force-reboot", false, "bypass rebootgate preflight when --reboot runs")
	cmd.Flags().BoolVar(&flags.noWait, "no-wait", false, "return immediately after scheduling the reboot instead of waiting for recovery")
	cmd.Flags().BoolVar(&flags.fix, "fix", false, "run doctor --fix security and managed-tool convergence after upgrading")
	return cmd
}

func (a *app) serverRebootCmd() *cobra.Command {
	var flags serverRebootFlags
	cmd := withScopedFlags(&cobra.Command{
		Use:   "reboot NAME",
		Short: "perform an OS-level gated reboot of a managed server over Tailscale SSH",
		Long: "Run workload survivability preflight and cleanly reboot the Ubuntu host over Tailscale SSH, " +
			"then wait for a new boot ID and post-reboot verification.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a.server = args[0]
			return a.runServerReboot(cmd.Context(), args[0], flags)
		},
	}, "config", "state", "namespace", "provider", "non-interactive", "dry-run", "yes")
	cmd.Flags().BoolVar(&flags.ifRequired, "if-required", false, "reboot only when /var/run/reboot-required exists")
	cmd.Flags().BoolVar(&flags.force, "force", false, "bypass rebootgate survivability preflight")
	cmd.Flags().BoolVar(&flags.noWait, "no-wait", false, "return immediately after scheduling the reboot")
	return cmd
}

func validateCheckModeConflicts(flags serverUpgradeFlags, dryRun bool) error {
	if !flags.check {
		return nil
	}
	switch {
	case flags.security:
		return errors.New("--check cannot be used with --security")
	case flags.reboot || flags.forceReboot || flags.noWait:
		return errors.New("--check cannot be used with reboot flags")
	case flags.fix:
		return errors.New("--check cannot be used with --fix")
	case dryRun:
		return errors.New("--check cannot be used with --dry-run")
	default:
		return nil
	}
}

func resolveUpgradeMode(flags serverUpgradeFlags, dryRun bool) (lifecycle.UpgradeMode, error) {
	if err := validateCheckModeConflicts(flags, dryRun); err != nil {
		return "", err
	}
	if dryRun && flags.fix {
		return "", errors.New("--fix cannot be used with --dry-run")
	}
	if !flags.reboot && (flags.forceReboot || flags.noWait) {
		return "", errors.New("--force-reboot and --no-wait require --reboot")
	}
	if flags.noWait && flags.fix {
		return "", errors.New("--no-wait cannot be used with --fix")
	}
	if flags.check {
		return lifecycle.UpgradeModeCheck, nil
	}
	if flags.security {
		return lifecycle.UpgradeModeSecurity, nil
	}
	return lifecycle.UpgradeModeAll, nil
}

func (a *app) confirmMaintenanceAction(actionLabel, promptMsg string) error {
	if a.yes {
		return nil
	}
	if a.nonInteractive {
		return fmt.Errorf("--yes required for non-interactive %s", actionLabel)
	}
	return a.confirm(promptMsg)
}

func (a *app) runServerUpgrade(ctx context.Context, name string, flags serverUpgradeFlags) error {
	mode, err := resolveUpgradeMode(flags, a.dryRun)
	if err != nil {
		return err
	}
	cfg, _, st, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return err
	}
	if st.Tailscale.Name == "" {
		return errors.New(missingTailscaleMaintenanceError)
	}
	if a.dryRun {
		row := serverUpgradeRow{
			Status:        maintenanceStatusPlanned,
			Action:        upgradeOperationAction,
			DryRun:        true,
			Namespace:     cfg.Namespace,
			Server:        targetServer(cfg.Server),
			User:          cfg.Admin.Username,
			Host:          st.Tailscale.Name,
			Mode:          mode,
			RebootPlanned: flags.reboot,
			FixPlanned:    flags.fix,
		}
		return writeJSON(a.stdout, row)
	}
	if flags.reboot {
		if err := a.confirmMaintenanceAction("upgrade --reboot", upgradeConfirmMessage); err != nil {
			return err
		}
	}
	return a.executeServerUpgrade(ctx, name, mode, flags, st)
}

func (a *app) executeServerUpgrade(ctx context.Context, name string, mode lifecycle.UpgradeMode, flags serverUpgradeFlags, initialState state.State) error {
	unlockWorkflow, err := lockServerArtifactWorkflow(ctx, initialState)
	if err != nil {
		return err
	}
	defer unlockWorkflow()
	cfg, stPath, st, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return err
	}
	if st.Tailscale.Name == "" {
		return errors.New(missingTailscaleMaintenanceError)
	}
	// Resolve credentials before any remote mutation so missing tokens fail
	// closed instead of leaving a half-completed upgrade --fix run.
	var creds credentials.Set
	if flags.fix {
		creds, _, err = a.ensureCredentials(cfg)
		if err != nil {
			return err
		}
	}
	sudoPassword, err := a.resolveSudoPassword(cfg)
	if err != nil {
		return err
	}
	redactor := redact.New(a.redactionSecrets(creds)...)
	opCtx, cancel := contextWithDefaultTimeout(ctx, upgradeCommandTimeout)
	defer cancel()
	progress := a.newCommandProgress()
	if err := progress.emit(progressPhaseUpgrade); err != nil {
		return err
	}
	opts := lifecycle.UpgradeOptions{Mode: mode, Reboot: flags.reboot, ForceReboot: flags.forceReboot, NoWait: flags.noWait}
	result, upgradeErr := a.upgradeServer(opCtx, cfg, st, sudoPassword, opts)
	row := upgradeRowFromResult(cfg, st, result, redactor)
	if upgradeErr != nil {
		return a.handleUpgradeError(row, upgradeErr, redactor)
	}
	if flags.fix {
		return a.finishUpgradeWithDoctorFix(opCtx, cfg, stPath, st, creds, sudoPassword, progress, row, redactor)
	}
	return redactor.Error(writeJSON(a.stdout, row))
}

func upgradeRowFromResult(cfg config.Config, st state.State, result lifecycle.UpgradeResult, redactor redact.Redactor) serverUpgradeRow {
	row := serverUpgradeRow{
		Status:                 maintenanceStatusComplete,
		Action:                 upgradeOperationAction,
		Namespace:              cfg.Namespace,
		Server:                 targetServer(cfg.Server),
		User:                   cfg.Admin.Username,
		Host:                   st.Tailscale.Name,
		Mode:                   result.Mode,
		PendingPackages:        result.PendingPackages,
		UpgradedPackages:       result.UpgradedPackages,
		RebootRequired:         result.RebootRequired,
		RebootRequiredPackages: result.RebootRequiredPackages,
	}
	if result.Reboot != nil {
		rebootCopy := redactRebootResult(*result.Reboot, redactor)
		row.Reboot = &rebootCopy
	}
	return row
}

func (a *app) handleUpgradeError(row serverUpgradeRow, upgradeErr error, redactor redact.Redactor) error {
	safeErr := redactor.Error(upgradeErr)
	var preflightErr *lifecycle.RebootPreflightError
	if errors.As(upgradeErr, &preflightErr) {
		row.Status = maintenanceStatusBlocked
		row.PreflightGaps = redactStrings(preflightErr.Gaps, redactor)
		return errors.Join(safeErr, redactor.Error(writeJSON(a.stdout, row)))
	}
	if row.Reboot != nil || len(row.UpgradedPackages) > 0 {
		row.Status = maintenanceStatusFailed
		return errors.Join(safeErr, redactor.Error(writeJSON(a.stdout, row)))
	}
	return safeErr
}

func (a *app) finishUpgradeWithDoctorFix(ctx context.Context, cfg config.Config, stPath string, st state.State, creds credentials.Set, sudoPassword string, progress *commandProgress, row serverUpgradeRow, redactor redact.Redactor) error {
	if err := progress.emit(progressPhaseDoctor); err != nil {
		return err
	}
	prevFix := a.doctorFix
	prevStatePath := a.doctorStatePath
	a.doctorFix = true
	a.doctorStatePath = stPath
	defer func() {
		a.doctorFix = prevFix
		a.doctorStatePath = prevStatePath
	}()
	report := a.doctorReport(ctx, cfg, st, creds, sudoPassword)
	summary := a.saveDoctorSummary(cfg, creds, report)
	row.Doctor = &summary
	if !report.Passed() {
		row.Status = maintenanceStatusFailed
		if err := writeJSON(a.stdout, row); err != nil {
			return errors.Join(errors.New(upgradeDoctorFailedError), redactor.Error(err))
		}
		return errors.New(upgradeDoctorFailedError)
	}
	return redactor.Error(writeJSON(a.stdout, row))
}

// saveDoctorSummary persists the complete redacted doctor report and returns
// its summary for embedding inside a composite command's single JSON document.
func (a *app) saveDoctorSummary(cfg config.Config, creds credentials.Set, report doctor.Report) doctor.Summary {
	redactor := redact.New(a.redactionSecrets(creds)...)
	report = report.RedactForOutput(a.redactionSecrets(creds)...)
	path, saveErr := report.Save(cfg.Namespace, targetServer(cfg.Server))
	if saveErr != nil {
		stderr := a.stderr
		if stderr == nil {
			stderr = io.Discard
		}
		_, _ = fmt.Fprintf(stderr, doctorLogWarning, redactor.String(saveErr.Error()))
		path = ""
	}
	return report.Summary(redactor.String(cfg.Namespace), redactor.String(targetServer(cfg.Server)), redactor.String(path))
}

func (a *app) runServerReboot(ctx context.Context, name string, flags serverRebootFlags) error {
	cfg, _, st, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return err
	}
	if st.Tailscale.Name == "" {
		return errors.New(missingTailscaleMaintenanceError)
	}
	if a.dryRun {
		row := serverRebootRow{
			Status:     maintenanceStatusPlanned,
			Action:     rebootOperationAction,
			DryRun:     true,
			Namespace:  cfg.Namespace,
			Server:     targetServer(cfg.Server),
			User:       cfg.Admin.Username,
			Host:       st.Tailscale.Name,
			IfRequired: flags.ifRequired,
			Force:      flags.force,
			NoWait:     flags.noWait,
			Gated:      !flags.force,
		}
		return writeJSON(a.stdout, row)
	}
	if err := a.confirmMaintenanceAction(rebootOperationAction, rebootConfirmMessage); err != nil {
		return err
	}
	return a.executeServerReboot(ctx, name, flags, st)
}

func (a *app) executeServerReboot(ctx context.Context, name string, flags serverRebootFlags, initialState state.State) error {
	unlockWorkflow, err := lockServerArtifactWorkflow(ctx, initialState)
	if err != nil {
		return err
	}
	defer unlockWorkflow()
	cfg, _, st, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return err
	}
	if st.Tailscale.Name == "" {
		return errors.New(missingTailscaleMaintenanceError)
	}
	sudoPassword, err := a.resolveSudoPassword(cfg)
	if err != nil {
		return err
	}
	redactor := redact.New(a.runtimeSecrets...)
	opCtx, cancel := contextWithDefaultTimeout(ctx, rebootCommandTimeout)
	defer cancel()
	progress := a.newCommandProgress()
	if err := progress.emit(progressPhaseReboot); err != nil {
		return err
	}
	opts := lifecycle.RebootOptions{IfRequired: flags.ifRequired, Force: flags.force, NoWait: flags.noWait}
	result, rebootErr := a.rebootServer(opCtx, cfg, st, sudoPassword, opts)
	row := rebootRowFromResult(cfg, st, flags, redactRebootResult(result, redactor))
	if rebootErr != nil {
		return a.handleRebootError(row, rebootErr, redactor)
	}
	return redactor.Error(writeJSON(a.stdout, row))
}

func rebootRowFromResult(cfg config.Config, st state.State, flags serverRebootFlags, result lifecycle.RebootResult) serverRebootRow {
	return serverRebootRow{
		Status:                 result.Status,
		Action:                 rebootOperationAction,
		Namespace:              cfg.Namespace,
		Server:                 targetServer(cfg.Server),
		User:                   cfg.Admin.Username,
		Host:                   st.Tailscale.Name,
		IfRequired:             flags.ifRequired,
		Force:                  flags.force,
		NoWait:                 flags.noWait,
		RebootRequired:         result.RebootRequired,
		RebootRequiredPackages: result.RebootRequiredPackages,
		Rebooted:               result.Rebooted,
		Gated:                  result.Gated,
		BeforeBootID:           result.BeforeBootID,
		AfterBootID:            result.AfterBootID,
		VerifyStatus:           result.VerifyStatus,
		VerifyFindings:         result.VerifyFindings,
	}
}

func (a *app) handleRebootError(row serverRebootRow, rebootErr error, redactor redact.Redactor) error {
	safeErr := redactor.Error(rebootErr)
	var preflightErr *lifecycle.RebootPreflightError
	if errors.As(rebootErr, &preflightErr) {
		row.Status = maintenanceStatusBlocked
		row.PreflightGaps = redactStrings(preflightErr.Gaps, redactor)
		return errors.Join(safeErr, redactor.Error(writeJSON(a.stdout, row)))
	}
	if row.BeforeBootID != "" {
		row.Status = maintenanceStatusFailed
		return errors.Join(safeErr, redactor.Error(writeJSON(a.stdout, row)))
	}
	return safeErr
}

func redactRebootResult(result lifecycle.RebootResult, redactor redact.Redactor) lifecycle.RebootResult {
	result.VerifyFindings = redactStrings(result.VerifyFindings, redactor)
	return result
}

func redactStrings(values []string, redactor redact.Redactor) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = redactor.String(value)
	}
	return out
}
