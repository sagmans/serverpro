package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/remote"
)

func TestRunBootstrapsTunnelAndLockdownOverRemote(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	r := &fakeRemote{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: &fakeHetzner{}, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: r}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.scripts) != 4 {
		t.Fatalf("expected remote probe, tunnel install, server tool bootstrap, and lockdown scripts, got %d: %q", len(r.scripts), r.scripts)
	}
	if r.scripts[0] != "true" || !strings.Contains(r.scripts[1], "cloudflared") || !strings.Contains(r.scripts[2], "serverpro-bootstrap-tools") || !strings.Contains(r.scripts[2], "@earendil-works/pi-coding-agent") || !strings.Contains(r.scripts[2], "mise bootstrap packages apply") || !strings.Contains(r.scripts[2], "mise --yes install") || !strings.Contains(r.scripts[2], "SERVERPRO_BOOTSTRAP_TMUX_VERSION") || !strings.Contains(r.scripts[3], "ufw default deny outgoing") || !strings.Contains(r.scripts[3], "ufw allow out 7844/tcp") {
		t.Fatalf("unexpected remote scripts: %q", r.scripts)
	}
}

func TestRunSkipsCloudflareBootstrapWhenIngressDisabled(t *testing.T) {
	cfg := config.Example("prod")
	cf := &fakeCloudflare{}
	r := &fakeRemote{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: &fakeHetzner{}, Tailscale: &fakeTailscale{}, Cloudflare: cf, Remote: r}})
	if err != nil {
		t.Fatal(err)
	}
	if cf.tokenRequests != 0 {
		t.Fatalf("disabled ingress requested Cloudflare tunnel token %d times", cf.tokenRequests)
	}
	if len(r.scripts) != 3 {
		t.Fatalf("expected remote probe, server tool bootstrap, and lockdown scripts, got %d: %q", len(r.scripts), r.scripts)
	}
	if r.scripts[0] != "true" || strings.Contains(r.scripts[1], "cloudflared") || !strings.Contains(r.scripts[1], "serverpro-bootstrap-tools") || !strings.Contains(r.scripts[1], "mise bootstrap packages apply") || !strings.Contains(r.scripts[1], "mise --yes install") || !strings.Contains(r.scripts[2], "ufw default deny outgoing") {
		t.Fatalf("unexpected remote scripts: %q", r.scripts)
	}
}

func TestRunnerWithTimeoutClonesKnownRemoteRunners(t *testing.T) {
	base := remote.TailscaleSSH{SudoPassword: "pw"}
	got, ok := remote.WithTimeout(base, 5*time.Second).(remote.TailscaleSSH)
	if !ok || got.Timeout != 5*time.Second || got.SudoPassword != "pw" || base.Timeout != 0 {
		t.Fatalf("bad timeout clone: got=%#v base=%#v", got, base)
	}
}

func TestWaitRemoteFailsFastOnSudoAuthError(t *testing.T) {
	r := &errorRemote{err: errors.New("sudo: 1 incorrect password attempt")}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := waitRemote(ctx, r, "deploy", "prod-01")
	if err == nil || !strings.Contains(err.Error(), "sudo validation failed") || strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected immediate sudo auth error, got %v", err)
	}
	if r.calls != 1 {
		t.Fatalf("wrong sudo password should not be retried, calls=%d", r.calls)
	}
}

type errorRemote struct {
	calls int
	err   error
}

func (r *errorRemote) Run(context.Context, string, string, string) (string, error) {
	r.calls++
	return "", r.err
}
