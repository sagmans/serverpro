package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func TestBootstrapToolsRunsSelectedTargetOnStateHost(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	st := state.State{Tailscale: state.TailscaleState{Name: "demo-web"}}
	r := &gitRemote{}
	if err := BootstrapTools(context.Background(), r, cfg, st, bootstraptools.TargetGit); err != nil {
		t.Fatal(err)
	}
	if len(r.scripts) != 1 {
		t.Fatalf("scripts = %d", len(r.scripts))
	}
	if r.user != cfg.Admin.Username || r.host != "demo-web" {
		t.Fatalf("bad remote target user=%q host=%q", r.user, r.host)
	}
	if !strings.Contains(r.scripts[0], "SERVERPRO_BOOTSTRAP_TARGET='git'") || !strings.Contains(r.scripts[0], "install_git") {
		t.Fatalf("missing git bootstrap script:\n%s", r.scripts[0])
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
