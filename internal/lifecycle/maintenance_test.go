package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/state"
)

type scriptedMaintenanceRunner struct {
	scripts []string
	outputs []string
	errs    []error
}

func (r *scriptedMaintenanceRunner) Run(_ context.Context, _, _, script string) (string, error) {
	idx := len(r.scripts)
	r.scripts = append(r.scripts, script)
	var out string
	if idx < len(r.outputs) {
		out = r.outputs[idx]
	}
	var err error
	if idx < len(r.errs) {
		err = r.errs[idx]
	}
	return out, err
}

func maintenanceFixture() (config.Config, state.State) {
	cfg := config.ExampleServer("demo", "web")
	cfg.Admin.Username = "deploy"
	st := state.State{Namespace: "demo", Server: "web", Tailscale: state.TailscaleState{Name: "demo-web.ts.net"}}
	return cfg, st
}

func TestUpgradeServerValidation(t *testing.T) {
	cfg, st := maintenanceFixture()

	if _, err := UpgradeServer(context.Background(), nil, cfg, st, UpgradeOptions{Mode: UpgradeModeAll}); err == nil {
		t.Fatal("expected error with nil runner")
	}
	if _, err := UpgradeServer(context.Background(), &scriptedMaintenanceRunner{}, cfg, st, UpgradeOptions{Mode: UpgradeMode("bogus")}); err == nil {
		t.Fatal("expected error with unsupported mode")
	}
	if _, err := UpgradeServer(context.Background(), &scriptedMaintenanceRunner{}, cfg, st, UpgradeOptions{Mode: UpgradeModeCheck, Reboot: true}); err == nil {
		t.Fatal("expected error when check combines with reboot")
	}
	if _, err := UpgradeServer(context.Background(), &scriptedMaintenanceRunner{}, cfg, st, UpgradeOptions{Mode: UpgradeModeAll, ForceReboot: true}); err == nil {
		t.Fatal("expected error when force-reboot is set without reboot")
	}
	errRunner := &scriptedMaintenanceRunner{errs: []error{errors.New("apt mirror unreachable")}}
	if _, err := UpgradeServer(context.Background(), errRunner, cfg, st, UpgradeOptions{Mode: UpgradeModeAll}); err == nil || !strings.Contains(err.Error(), "upgrade all") {
		t.Fatalf("expected wrapped upgrade error, got %v", err)
	}
}

func TestUpgradeServerCheckAndSecurityModes(t *testing.T) {
	cfg, st := maintenanceFixture()

	checkRunner := &scriptedMaintenanceRunner{
		outputs: []string{
			"SERVERPRO_PENDING_PKG=libsqlite3-0\nSERVERPRO_PENDING_PKG=docker-ce\nSERVERPRO_REBOOT_REQUIRED=1\nSERVERPRO_REBOOT_PKG=linux-image-6.8.0-150-generic\n",
		},
	}
	got, err := UpgradeServer(context.Background(), checkRunner, cfg, st, UpgradeOptions{Mode: UpgradeModeCheck})
	if err != nil {
		t.Fatalf("check upgrade failed: %v", err)
	}
	if len(got.PendingPackages) != 2 || !got.RebootRequired || len(got.RebootRequiredPackages) != 1 {
		t.Fatalf("unexpected check result: %+v", got)
	}
	if !strings.Contains(checkRunner.scripts[0], "apt-get -s -o Debug::NoLocking=1 -o Dir::Etc::Preferences=\"$tmp_dir/preferences\" --with-new-pkgs upgrade") {
		t.Fatalf("check script missing apt simulation with temporary preferences:\n%s", checkRunner.scripts[0])
	}
	if !strings.Contains(checkRunner.scripts[0], "for pkg in tailscale cloudflared;") || !strings.Contains(checkRunner.scripts[0], "Pin-Priority: 1001") {
		t.Fatalf("check script missing temporary hold for tailscale and cloudflared:\n%s", checkRunner.scripts[0])
	}

	secRunner := &scriptedMaintenanceRunner{
		outputs: []string{
			"SERVERPRO_UPGRADED_PKG=libbrotli1=1.1.0-2ubuntu0.1\nSERVERPRO_REBOOT_REQUIRED=0\n",
		},
	}
	gotSec, err := UpgradeServer(context.Background(), secRunner, cfg, st, UpgradeOptions{Mode: UpgradeModeSecurity})
	if err != nil {
		t.Fatalf("security upgrade failed: %v", err)
	}
	if len(gotSec.UpgradedPackages) != 1 || gotSec.RebootRequired {
		t.Fatalf("unexpected security result: %+v", gotSec)
	}
	if !strings.Contains(secRunner.scripts[0], "NEEDRESTART_SUSPEND=1 unattended-upgrade -v") {
		t.Fatalf("security script missing suspended unattended-upgrade:\n%s", secRunner.scripts[0])
	}
	if !strings.Contains(secRunner.scripts[0], needrestartTailscaleRule) {
		t.Fatalf("security script missing tailscaled needrestart exclusion:\n%s", secRunner.scripts[0])
	}
}

func TestUpgradeServerTriggersGatedRebootWhenRequired(t *testing.T) {
	cfg, st := maintenanceFixture()
	runner := &scriptedMaintenanceRunner{
		outputs: []string{
			"SERVERPRO_UPGRADED_PKG=linux-image-6.8.0-150-generic=6.8.0-150.150\nSERVERPRO_REBOOT_REQUIRED=1\nSERVERPRO_REBOOT_PKG=linux-image-6.8.0-150-generic\n",
			"SERVERPRO_BOOT_ID=boot-1\nSERVERPRO_REBOOT_REQUIRED=1\nSERVERPRO_REBOOT_PKG=linux-image-6.8.0-150-generic\nSERVERPRO_REBOOT_GATED=1\nSERVERPRO_REBOOT_ACTION=rebooting\n",
			"",
			"SERVERPRO_BOOT_ID=boot-2\nSERVERPRO_VERIFY_ACTIVE=1\n",
			"SERVERPRO_BOOT_ID=boot-2\nSERVERPRO_VERIFY_ACTIVE=0\nSERVERPRO_VERIFY_BOOT=boot-2\nSERVERPRO_VERIFY_STATUS=ok\n",
		},
		errs: []error{nil, nil, errors.New("connection refused during reboot"), nil, nil},
	}
	got, err := UpgradeServer(context.Background(), runner, cfg, st, UpgradeOptions{
		Mode:   UpgradeModeAll,
		Reboot: true,
		Wait:   func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("upgrade with reboot failed: %v", err)
	}
	if got.Reboot == nil || got.Reboot.Status != RebootStatusComplete || !got.Reboot.Gated || got.Reboot.AfterBootID != "boot-2" || got.Reboot.VerifyStatus != RebootVerifyStatusOK {
		t.Fatalf("unexpected reboot result: %+v", got.Reboot)
	}
	if !strings.Contains(runner.scripts[0], "--with-new-pkgs") {
		t.Fatalf("full upgrade script missing --with-new-pkgs:\n%s", runner.scripts[0])
	}
	wantGatedCmd := "--unit=" + rebootgate.OnDemandUnitPrefix + "$$ --description='serverpro gated reboot' " + shell.Quote(rebootgate.ScriptPath) + " window"
	if !strings.Contains(runner.scripts[1], wantGatedCmd) {
		t.Fatalf("gated reboot trigger must pair %s with the rebootgate window command:\n%s", wantGatedCmd, runner.scripts[1])
	}
}

func TestRebootServerSkipAndPreflightPaths(t *testing.T) {
	cfg, st := maintenanceFixture()

	if _, err := RebootServer(context.Background(), nil, cfg, st, RebootOptions{}); err == nil {
		t.Fatal("expected error with nil runner")
	}

	// Skipped when --if-required and no reboot pending.
	skipRunner := &scriptedMaintenanceRunner{
		outputs: []string{"SERVERPRO_BOOT_ID=boot-1\nSERVERPRO_REBOOT_REQUIRED=0\nSERVERPRO_REBOOT_ACTION=skipped\n"},
	}
	skipRes, err := RebootServer(context.Background(), skipRunner, cfg, st, RebootOptions{IfRequired: true})
	if err != nil || skipRes.Status != RebootStatusSkipped || skipRes.Rebooted {
		t.Fatalf("unexpected skip result=%+v err=%v", skipRes, err)
	}

	// Blocked when preflight reports survivability gaps.
	blockedRunner := &scriptedMaintenanceRunner{
		outputs: []string{"SERVERPRO_BOOT_ID=boot-1\nSERVERPRO_PREFLIGHT_GAP=container app: restart policy is no\nSERVERPRO_REBOOT_ACTION=blocked\n"},
	}
	_, err = RebootServer(context.Background(), blockedRunner, cfg, st, RebootOptions{})
	var preflightErr *RebootPreflightError
	if !errors.As(err, &preflightErr) || len(preflightErr.Gaps) != 1 || !strings.Contains(preflightErr.Error(), "restart policy is no") {
		t.Fatalf("expected RebootPreflightError, got %v", err)
	}

	// NoWait returns immediately with rebooting status.
	noWaitRunner := &scriptedMaintenanceRunner{
		outputs: []string{"SERVERPRO_BOOT_ID=boot-1\nSERVERPRO_REBOOT_GATED=0\nSERVERPRO_REBOOT_ACTION=rebooting\n"},
	}
	noWaitRes, err := RebootServer(context.Background(), noWaitRunner, cfg, st, RebootOptions{NoWait: true})
	if err != nil || noWaitRes.Status != RebootStatusRebooting || !noWaitRes.Rebooted {
		t.Fatalf("unexpected no-wait result=%+v err=%v", noWaitRes, err)
	}
}

func TestRebootServerForceAndFailurePaths(t *testing.T) {
	cfg, st := maintenanceFixture()

	// Forced ungated reboot completes once boot ID changes without waiting for verify.
	forceRunner := &scriptedMaintenanceRunner{
		outputs: []string{
			"SERVERPRO_BOOT_ID=boot-a\nSERVERPRO_REBOOT_REQUIRED=0\nSERVERPRO_REBOOT_GATED=0\nSERVERPRO_REBOOT_ACTION=rebooting\n",
			"SERVERPRO_BOOT_ID=boot-a\nSERVERPRO_VERIFY_ACTIVE=0\n",
			"SERVERPRO_BOOT_ID=boot-b\nSERVERPRO_VERIFY_ACTIVE=0\n",
		},
	}
	forceRes, err := RebootServer(context.Background(), forceRunner, cfg, st, RebootOptions{
		Force: true,
		Wait:  func(context.Context) error { return nil },
	})
	if err != nil || forceRes.Status != RebootStatusComplete || forceRes.VerifyStatus != RebootVerifyStatusNotApplicable || forceRes.AfterBootID != "boot-b" {
		t.Fatalf("unexpected force reboot result=%+v err=%v", forceRes, err)
	}
	if strings.Contains(forceRunner.scripts[0], "preflight_status") {
		t.Fatalf("force reboot should skip preflight:\n%s", forceRunner.scripts[0])
	}

	// Degraded post-reboot verification fails closed with findings.
	degradedRunner := &scriptedMaintenanceRunner{
		outputs: []string{
			"SERVERPRO_BOOT_ID=boot-1\nSERVERPRO_REBOOT_REQUIRED=1\nSERVERPRO_REBOOT_GATED=1\nSERVERPRO_REBOOT_ACTION=rebooting\n",
			"SERVERPRO_BOOT_ID=boot-2\nSERVERPRO_VERIFY_ACTIVE=0\nSERVERPRO_VERIFY_BOOT=boot-2\nSERVERPRO_VERIFY_STATUS=degraded\nSERVERPRO_VERIFY_FINDING=unit docker.service failed\n",
		},
	}
	degradedRes, err := RebootServer(context.Background(), degradedRunner, cfg, st, RebootOptions{
		Wait: func(context.Context) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "degraded") || degradedRes.VerifyStatus != RebootVerifyStatusDegraded || len(degradedRes.VerifyFindings) != 1 {
		t.Fatalf("expected degraded verification error, got res=%+v err=%v", degradedRes, err)
	}

	// Unexpected or empty trigger output fails closed.
	badRunner := &scriptedMaintenanceRunner{outputs: []string{"SERVERPRO_REBOOT_ACTION=rebooting\n"}}
	if _, err := RebootServer(context.Background(), badRunner, cfg, st, RebootOptions{NoWait: true}); err == nil {
		t.Fatal("expected error when pre-reboot boot ID is missing")
	}
	unknownRunner := &scriptedMaintenanceRunner{outputs: []string{"noise\n"}}
	if _, err := RebootServer(context.Background(), unknownRunner, cfg, st, RebootOptions{NoWait: true}); err == nil {
		t.Fatal("expected error on unknown reboot action")
	}

	// Context cancellation during recovery wait fails cleanly.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelRunner := &scriptedMaintenanceRunner{
		outputs: []string{"SERVERPRO_BOOT_ID=boot-1\nSERVERPRO_REBOOT_GATED=0\nSERVERPRO_REBOOT_ACTION=rebooting\n"},
	}
	if _, err := RebootServer(cancelCtx, cancelRunner, cfg, st, RebootOptions{}); err == nil {
		t.Fatal("expected cancellation error during wait")
	}
}
