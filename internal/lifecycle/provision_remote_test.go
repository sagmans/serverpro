package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/sagmans/serverpro/internal/tunnel"
)

func TestRunBootstrapsTunnelAndLockdownOverRemote(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	r := &fakeRemote{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPassword: testAdminPassword, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: &fakeHetzner{}, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: r}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.scripts) != 7 {
		t.Fatalf("expected remote probe, admin password, tunnel install, server tool bootstrap, lockdown, gated reboot, and OpenSSH-off scripts, got %d: %q", len(r.scripts), r.scripts)
	}
	rebootGate, err := rebootgate.InstallScript(cfg.Reboot)
	if err != nil {
		t.Fatal(err)
	}
	if r.scripts[5] != rebootGate {
		t.Fatalf("remote script before OpenSSH-off is not the gated reboot install: %q", r.scripts[5])
	}
	if r.scripts[6] != OpenSSHDisableScript(cfg.Admin.Username, nil) {
		t.Fatalf("last remote script is not the OpenSSH-off step: %q", r.scripts[6])
	}
	if r.scripts[0] != "true" || r.scripts[1] != AdminPasswordScript(cfg.Admin.Username) || !strings.Contains(r.scripts[2], "cloudflared") || !strings.Contains(r.scripts[3], "serverpro-bootstrap-tools") || !strings.Contains(r.scripts[3], "@earendil-works/pi-coding-agent") || !strings.Contains(r.scripts[3], "bootstrap packages apply --yes") || !strings.Contains(r.scripts[3], "bootstrap packages upgrade --yes") || !strings.Contains(r.scripts[3], "mise --yes install") || !strings.Contains(r.scripts[3], "SERVERPRO_BOOTSTRAP_TMUX_VERSION") || !strings.Contains(r.scripts[4], "ufw default deny outgoing") || !strings.Contains(r.scripts[4], "ufw allow out 7844/tcp") {
		t.Fatalf("unexpected remote scripts: %q", r.scripts)
	}
}

func TestRunSkipsCloudflareBootstrapWhenIngressDisabled(t *testing.T) {
	cfg := config.Example("prod")
	cf := &fakeCloudflare{}
	r := &fakeRemote{}
	_, err := Run(context.Background(), Options{Config: cfg, AdminPassword: testAdminPassword, Creds: credentials.Set{Tailscale: "ts-api"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: &fakeHetzner{}, Tailscale: &fakeTailscale{}, Cloudflare: cf, Remote: r}})
	if err != nil {
		t.Fatal(err)
	}
	if cf.tokenRequests != 0 {
		t.Fatalf("disabled ingress requested Cloudflare tunnel token %d times", cf.tokenRequests)
	}
	if len(r.scripts) != 6 {
		t.Fatalf("expected remote probe, admin password, server tool bootstrap, lockdown, gated reboot, and OpenSSH-off scripts, got %d: %q", len(r.scripts), r.scripts)
	}
	if r.scripts[0] != "true" || r.scripts[1] != AdminPasswordScript(cfg.Admin.Username) || strings.Contains(r.scripts[2], "cloudflared") || !strings.Contains(r.scripts[2], "serverpro-bootstrap-tools") || !strings.Contains(r.scripts[2], "bootstrap packages apply --yes") || !strings.Contains(r.scripts[2], "bootstrap packages upgrade --yes") || !strings.Contains(r.scripts[2], "mise --yes install") || !strings.Contains(r.scripts[3], "ufw default deny outgoing") {
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

func TestWaitRemoteFailsFastOnAutomationNodeNotReady(t *testing.T) {
	// WHY: a down or untagged automation node does not heal while create
	// waits, so retrying would only hide the real cause behind a timeout.
	r := &errorRemote{err: fmt.Errorf("%w: backend state %q", remote.ErrAutomationNodeNotReady, "Stopped")}
	err := waitRemoteWithPoll(context.Background(), r, "deploy", "prod-01", func(context.Context) error {
		t.Fatal("node fault must not reach the poll wait")
		return nil
	})
	if !errors.Is(err, remote.ErrAutomationNodeNotReady) || r.calls != 1 {
		t.Fatalf("calls=%d error=%v", r.calls, err)
	}
}

func TestWaitRemoteRetriesAutomationPeerNotReady(t *testing.T) {
	// WHY: a new server joins the netmap and gets host keys some seconds
	// after create, so a peer fault must be waited out, not fatal.
	peerErr := fmt.Errorf("%w: no peer %s", remote.ErrAutomationPeerNotReady, "prod-01")
	r := &sequenceRemote{errors: []error{peerErr, peerErr, nil}}
	err := waitRemoteWithPoll(context.Background(), r, "deploy", "prod-01", func(context.Context) error { return nil })
	if err != nil || r.calls != 3 {
		t.Fatalf("calls=%d error=%v", r.calls, err)
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

func (r *sequenceRemote) RunWithInput(ctx context.Context, user, host, script, _ string) (string, error) {
	return r.Run(ctx, user, host, script)
}

type errorRemote struct {
	calls int
	err   error
}

func (r *errorRemote) Run(context.Context, string, string, string) (string, error) {
	r.calls++
	return "", r.err
}

// deadlineRemote records each script's context deadline so tests can prove
// which remote steps outlive the transport's short default.
type deadlineRemote struct {
	fakeRemote
	deadlines map[string]time.Duration
}

func (d *deadlineRemote) Run(ctx context.Context, user, host, script string) (string, error) {
	if deadline, ok := ctx.Deadline(); ok {
		if d.deadlines == nil {
			d.deadlines = map[string]time.Duration{}
		}
		d.deadlines[script] = time.Until(deadline)
	}
	return d.fakeRemote.Run(ctx, user, host, script)
}

// TestBootstrapRemoteNetworkGivesTunnelInstallTheAptDeadline guards the
// provisioning install: first-boot apt-daily can hold both apt locks, and the
// transport default would cut off a wait the install script survives.
func TestBootstrapRemoteNetworkGivesTunnelInstallTheAptDeadline(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	st := state.State{}
	st.Tailscale.Name = "prod-01"
	st.Cloudflare.TunnelID = "tunnel-id"
	r := &deadlineRemote{}
	if err := bootstrapRemoteNetwork(context.Background(), r, &fakeCloudflare{}, cfg, st); err != nil {
		t.Fatal(err)
	}
	var install string
	for _, script := range r.scripts {
		if strings.Contains(script, "install_cloudflared_candidate") {
			install = script
		}
	}
	deadline, ok := r.deadlines[install]
	if install == "" || !ok {
		t.Fatalf("tunnel install not run with a deadline: %q", r.scripts)
	}
	if deadline < tunnel.ScriptTimeout-time.Minute || deadline > tunnel.ScriptTimeout {
		t.Fatalf("tunnel install deadline = %v, want about %v", deadline, tunnel.ScriptTimeout)
	}
}
