package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/network"
	"github.com/sagmans/serverpro/internal/state"
)

func TestBootstrapToolsRunsSelectedTargetOnStateHost(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	st := state.State{Tailscale: state.TailscaleState{Name: "demo-web"}}
	r := &gitRemote{}
	if err := BootstrapTools(context.Background(), r, cfg, st, bootstraptools.TargetGit); err != nil {
		t.Fatal(err)
	}
	// Bootstrap is the recovery after a create that stopped before the
	// lockdown, so the tools run first and the lockdown converges egress last.
	if len(r.scripts) != 2 || r.scripts[1] != network.LockdownScript(cfg) {
		t.Fatalf("scripts = %d, want the tools then the egress lockdown", len(r.scripts))
	}
	if r.user != cfg.Admin.Username || r.host != "demo-web" {
		t.Fatalf("bad remote target user=%q host=%q", r.user, r.host)
	}
	if !strings.Contains(r.scripts[0], "SERVERPRO_BOOTSTRAP_TARGET='git'") || !strings.Contains(r.scripts[0], "install_git") {
		t.Fatalf("missing git bootstrap script:\n%s", r.scripts[0])
	}

	cfg.Network.Egress.PhaseLockdownAfterBootstrap = false
	r = &gitRemote{}
	if err := BootstrapTools(context.Background(), r, cfg, st, bootstraptools.TargetGit); err != nil || len(r.scripts) != 1 {
		t.Fatalf("err=%v scripts=%d, want only the tools when config defers the lockdown", err, len(r.scripts))
	}
}

func TestBootstrapToolsRejectsMissingRemoteHost(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	if err := BootstrapTools(context.Background(), nil, cfg, state.State{}, bootstraptools.TargetAll); err == nil || !strings.Contains(err.Error(), "remote host unavailable") {
		t.Fatalf("expected missing remote error, got %v", err)
	}
}

func TestBootstrapToolsWrapsRemoteErrors(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	st := state.State{Tailscale: state.TailscaleState{Name: "demo-web"}}
	r := &gitRemote{err: errors.New("apt failed")}
	err := BootstrapTools(context.Background(), r, cfg, st, bootstraptools.TargetDocker)
	if err == nil || !strings.Contains(err.Error(), "bootstrap docker") || !strings.Contains(err.Error(), "apt failed") {
		t.Fatalf("expected wrapped docker error, got %v", err)
	}
}

func TestBootstrapRemoteNetworkFailsClosedWithoutRemoteHost(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	// Cloud-init leaves outgoing open, so a skipped remote phase must not
	// complete a create that promised restricted egress.
	if err := bootstrapRemoteNetwork(context.Background(), nil, nil, cfg, state.State{}); err == nil || !strings.Contains(err.Error(), "egress lockdown") {
		t.Fatalf("err = %v, want the missing lockdown", err)
	}
	cfg.Network.Egress.Mode = config.EgressModeOpen
	if err := bootstrapRemoteNetwork(context.Background(), nil, nil, cfg, state.State{}); err != nil {
		t.Fatalf("open egress needs no lockdown, got %v", err)
	}
}
