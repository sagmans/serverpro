package doctor

import (
	"context"
	"slices"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/state"
)

func doctorState(cfg config.Config, ipv4, ipv6 string) state.State {
	return state.State{
		Namespace: cfg.Project,
		Project:   cfg.Project,
		Server:    cfg.Server,
		Compute: state.ComputeState{
			Provider:      "hetzner",
			Account:       "prod",
			ID:            "2",
			Name:          "prod-01",
			Location:      "fsn1",
			Size:          "cpx22",
			PublicIPv4:    ipv4,
			PublicIPv6:    ipv6,
			ProviderState: map[string]string{"access_policy_id": "1"},
		},
		Tailscale:  state.TailscaleState{Name: "prod-01"},
		Cloudflare: state.CloudflareState{TunnelID: "tun1", Name: "prod-tunnel"},
		Labels:     ownership.ProviderLabels(cfg.Project, cfg.Server, nil),
	}
}

func TestRunSkipsIngressProbeWhenNoTunnelConfigured(t *testing.T) {
	cfg := config.Example("prod")
	report := Run(context.Background(), cfg, state.State{Project: "prod", Server: cfg.Server, Compute: state.ComputeState{Provider: "hetzner", Account: "prod", ID: "2", Name: "prod-01", PublicIPv4: "192.0.2.10", ProviderState: map[string]string{"access_policy_id": "1"}}, Tailscale: state.TailscaleState{Name: "prod-01"}, Labels: ownership.ProviderLabels("prod", cfg.Server, nil)}, credentials.Set{}, Clients{Compute: fakeCompute{}})
	if !report.Passed() {
		t.Fatalf("optional ingress should not fail doctor when no tunnel is configured: %+v", report.Results)
	}
	if !hasResult(report, "ingress", Skip, "no public ingress configured") {
		t.Fatalf("missing optional ingress skip: %+v", report.Results)
	}
}

func TestRunChecksSeparateSecuritySettings(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	r := &fakeRemote{}
	report := Run(context.Background(), cfg, doctorState(cfg, "192.0.2.10", ""), credentials.Set{Tailscale: "ts-token-long"}, Clients{Compute: fakeCompute{}, Tailscale: fakeTailscale{}, Cloudflare: fakeCloudflare{}, Remote: r})
	for _, want := range []string{"grep -Fx 'permitrootlogin no'", "grep -Fx 'passwordauthentication no'", "grep -Fx 'kbdinteractiveauthentication no'", "ChallengeResponseAuthentication no", "grep -Fx 'x11forwarding no'", "grep -Fx 'allowagentforwarding no'", "grep -Fx 'allowtcpforwarding no'", "grep -Fx 'permittunnel no'", "PermitOpen none", "Status: active", "Default: deny (incoming)", "ALLOW IN"} {
		if !hasCommand(r.commands, want) {
			t.Fatalf("missing security check %q in %#v", want, r.commands)
		}
	}
	for _, want := range []string{"sshd root login", "sshd password auth", "sshd keyboard-interactive auth", "sshd challenge-response auth", "sshd x11 forwarding", "sshd agent forwarding", "sshd tcp forwarding", "sshd tunnel forwarding", "sshd open forwarding", "ufw active", "ufw default deny incoming", "ufw ssh ingress"} {
		if !hasResult(report, want, Pass, "ok") {
			t.Fatalf("missing passing result %q in %+v", want, report.Results)
		}
	}
}

func TestRunChecksIPv6WhenStateHasHostAddress(t *testing.T) {
	cfg := config.Example("prod")
	report := Run(context.Background(), cfg, doctorState(cfg, "", "2001:db8::1"), credentials.Set{}, Clients{})
	if !hasEvidence(report, "2001:db8::1 tcp/22") {
		t.Fatal("missing IPv6 public SSH probe")
	}
}

func TestPublicSSHAddressesPreferLiveProviderStatus(t *testing.T) {
	cfg := config.Example("prod")
	st := doctorState(cfg, "0.0.0.0", "")
	status := fakeCompute{status: compute.ServerStatus{PublicIPv4: "192.0.2.10", Record: compute.ServerRecord{ID: "2", Name: "prod-01", PublicIPv4: "192.0.2.10", Labels: ownership.ProviderLabels(cfg.Project, cfg.Server, nil)}}}
	got := publicSSHAddresses(context.Background(), cfg, st, compute.Account{Name: "prod", Provider: "hetzner", Token: "token"}, status)
	if !slices.Contains(got, "192.0.2.10") || slices.Contains(got, "0.0.0.0") {
		t.Fatalf("publicSSHAddresses = %+v", got)
	}
}

func TestRunIncludesTargetScopedProviderInventory(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	report := Run(context.Background(), cfg, doctorState(cfg, "192.0.2.10", "2001:db8::1"), credentials.Set{Tailscale: "ts-token-long"}, Clients{Compute: fakeCompute{}, Tailscale: fakeTailscale{}, Cloudflare: fakeCloudflare{}, Remote: &fakeRemote{}})
	for _, want := range []struct {
		name  string
		value string
	}{
		{name: "compute server", value: "size=cpx22"},
		{name: "compute server", value: "power=running"},
		{name: "tailscale node", value: "api_reported_online=false"},
		{name: "tailscale node", value: "control_connected=true"},
		{name: "cloudflare tunnel", value: "status=healthy"},
	} {
		if !hasInventory(report, want.name, want.value) {
			t.Fatalf("missing inventory %s containing %q in %+v", want.name, want.value, report.Inventory)
		}
	}
	if !hasResult(report, "tailscale node", Pass, "api_reported_online=false control_connected=true") || !hasResult(report, "tailscale node", Pass, "ssh=ok") {
		t.Fatalf("missing tailscale API/control/SSH status in check evidence: %+v", report.Results)
	}
}

func TestRunHandlesNilProviderClients(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	report := Run(context.Background(), cfg, doctorState(cfg, "192.0.2.10", ""), credentials.Set{Tailscale: "ts-token-long"}, Clients{})
	for _, want := range []struct {
		name     string
		evidence string
	}{
		{name: "compute server", evidence: "no compute provider"},
		{name: "tailscale node", evidence: "no tailscale client"},
		{name: "cloudflare tunnel", evidence: "no cloudflare client"},
	} {
		if !hasResult(report, want.name, Fail, want.evidence) {
			t.Fatalf("missing nil-client failure for %s in %+v", want.name, report.Results)
		}
	}
}
