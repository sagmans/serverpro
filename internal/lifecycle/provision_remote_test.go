package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
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
	if r.scripts[0] != "true" || !strings.Contains(r.scripts[1], "cloudflared") || !strings.Contains(r.scripts[2], "serverpro-bootstrap-tools") || !strings.Contains(r.scripts[2], "@earendil-works/pi-coding-agent") || !strings.Contains(r.scripts[2], "bootstrap packages apply --yes") || !strings.Contains(r.scripts[2], "bootstrap packages upgrade --yes") || !strings.Contains(r.scripts[2], "mise --yes install") || !strings.Contains(r.scripts[2], "SERVERPRO_BOOTSTRAP_TMUX_VERSION") || !strings.Contains(r.scripts[3], "ufw default deny outgoing") || !strings.Contains(r.scripts[3], "ufw allow out 7844/tcp") {
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
	if r.scripts[0] != "true" || strings.Contains(r.scripts[1], "cloudflared") || !strings.Contains(r.scripts[1], "serverpro-bootstrap-tools") || !strings.Contains(r.scripts[1], "bootstrap packages apply --yes") || !strings.Contains(r.scripts[1], "bootstrap packages upgrade --yes") || !strings.Contains(r.scripts[1], "mise --yes install") || !strings.Contains(r.scripts[2], "ufw default deny outgoing") {
		t.Fatalf("unexpected remote scripts: %q", r.scripts)
	}
}

func TestBootstrapRemoteNetworkLocksDownWhenPhaseFlagOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serverpro.yaml")
	body := []byte("namespace: prod\nadmin:\n  username: deploy\nnetwork:\n  egress:\n    mode: restricted\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadPartial(path)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRemote{}
	st := state.State{Tailscale: state.TailscaleState{Name: "prod-web"}}
	if err := bootstrapRemoteNetwork(context.Background(), r, nil, cfg, st); err != nil {
		t.Fatal(err)
	}
	if len(r.scripts) != 2 || !strings.Contains(r.scripts[1], "ufw default deny outgoing") {
		t.Fatalf("omitted lockdown phase skipped lockdown script: %q", r.scripts)
	}
}

func TestWaitRemoteRetriesWithoutSleeping(t *testing.T) {
	r := &sequenceRemote{errors: []error{errors.New("not ready"), nil}}
	err := waitRemoteWithPoll(context.Background(), r, "deploy", "prod-01", func(context.Context) error { return nil })
	if err != nil || r.calls != 2 {
		t.Fatalf("calls=%d error=%v", r.calls, err)
	}
}

func TestWaitRemoteReturnsPollCancellation(t *testing.T) {
	r := &errorRemote{err: errors.New("not ready")}
	err := waitRemoteWithPoll(context.Background(), r, "deploy", "prod-01", func(context.Context) error { return context.Canceled })
	if !errors.Is(err, context.Canceled) || r.calls != 1 {
		t.Fatalf("calls=%d error=%v", r.calls, err)
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

type sequenceRemote struct {
	calls  int
	errors []error
}

func (r *sequenceRemote) Run(context.Context, string, string, string) (string, error) {
	err := r.errors[r.calls]
	r.calls++
	return "", err
}

type errorRemote struct {
	calls int
	err   error
}

func (r *errorRemote) Run(context.Context, string, string, string) (string, error) {
	r.calls++
	return "", r.err
}
