package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunCheckpointFailuresReturnTypedPhaseAndResourceSnapshot(t *testing.T) {
	saveErr := errors.New("checkpoint unavailable")
	tests := []struct {
		name  string
		phase ProvisionPhase
		fail  func(state.State) bool
		check func(*testing.T, state.State, ProvisionResources)
	}{
		{name: "tailscale policy", phase: ProvisionPhaseTailscalePolicy, fail: func(st state.State) bool { return st.Tailscale.PolicySSHRule }},
		{name: "cloudflare tunnel", phase: ProvisionPhaseCloudflareTunnel, fail: func(st state.State) bool { return st.Cloudflare.TunnelID != "" }, check: func(t *testing.T, st state.State, resources ProvisionResources) {
			if st.Cloudflare.TunnelID != "tun1" || resources.CloudflareTunnelID != "tun1" {
				t.Fatalf("tunnel mutation missing from failure evidence: state=%+v resources=%+v", st.Cloudflare, resources)
			}
		}},
		{name: "tailscale auth key", phase: ProvisionPhaseTailscaleAuthKey, fail: func(st state.State) bool { return st.Tailscale.AuthKeyID != "" }, check: func(t *testing.T, st state.State, resources ProvisionResources) {
			if st.Tailscale.AuthKeyID != "k1" || resources.TailscaleAuthKeyID != "k1" {
				t.Fatalf("auth-key mutation missing from failure evidence: state=%+v resources=%+v", st.Tailscale, resources)
			}
		}},
		{name: "compute", phase: ProvisionPhaseCompute, fail: func(st state.State) bool { return st.Compute.ID != "" }, check: func(t *testing.T, st state.State, resources ProvisionResources) {
			if st.Compute.ID != "2" || resources.ComputeID != "2" || managedResourceID(resources.ManagedResources) != "1" {
				t.Fatalf("compute mutation missing from failure evidence: state=%+v resources=%+v", st.Compute, resources)
			}
			st.Compute.ManagedResources[0].ID = "changed"
			if managedResourceID(resources.ManagedResources) != "1" {
				t.Fatal("provision failure resource snapshot aliases mutable state")
			}
		}},
		{name: "tailscale device", phase: ProvisionPhaseTailscaleDevice, fail: func(st state.State) bool { return st.Tailscale.NodeID != "" }, check: func(t *testing.T, st state.State, resources ProvisionResources) {
			if st.Tailscale.NodeID != "d1" || resources.TailscaleNodeID != "d1" {
				t.Fatalf("device mutation missing from failure evidence: state=%+v resources=%+v", st.Tailscale, resources)
			}
		}},
		{name: "completion", phase: ProvisionPhaseComplete, fail: func(st state.State) bool { return len(st.Validations) != 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.ExampleServer("prod", "web")
			cfg.Cloudflare.AccountID = "acc"
			cfg.Cloudflare.Tunnel.Enabled = true
			failed := false
			ts := &fakeTailscale{}
			st, err := Run(context.Background(), Options{
				Config: cfg, AdminPasswordHash: testAdminPasswordHash,
				Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: provisionStatePath(t),
				Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}},
				saveState: func(_ string, got state.State) error {
					if !failed && test.fail(got) {
						failed = true
						return saveErr
					}
					return nil
				},
			})
			var provisionErr *ProvisionError
			if !errors.As(err, &provisionErr) || !errors.Is(err, saveErr) {
				t.Fatalf("expected typed checkpoint error preserving cause, got %T %v", err, err)
			}
			if provisionErr.Phase != test.phase {
				t.Fatalf("phase=%q want=%q", provisionErr.Phase, test.phase)
			}
			if !strings.Contains(err.Error(), string(test.phase)) {
				t.Fatalf("error omits phase: %v", err)
			}
			if test.check != nil {
				test.check(t, st, provisionErr.Resources)
			}
			if test.phase == ProvisionPhaseTailscaleAuthKey && !strings.Contains(strings.Join(ts.calls, ","), "delete-key") {
				t.Fatalf("fresh auth key was not compensated after checkpoint failure: %v", ts.calls)
			}
		})
	}
}

func TestProvisionErrorExposesOnlyKnownResourceIDs(t *testing.T) {
	st := state.State{Compute: state.ComputeState{ManagedResources: []compute.ManagedResourceRef{{Kind: compute.ManagedResourceAccessPolicy, ID: "fw-9"}}, ProviderState: map[string]string{"opaque_secret": "sensitive-provider-value"}}}
	err := newProvisionError(ProvisionPhaseCompute, st, errors.New("checkpoint failed"))
	var provisionErr *ProvisionError
	if !errors.As(err, &provisionErr) {
		t.Fatalf("expected typed provision error, got %T", err)
	}
	if managedResourceID(provisionErr.Resources.ManagedResources) != "fw-9" {
		t.Fatalf("known resource id missing: %+v", provisionErr.Resources)
	}
	if strings.Contains(err.Error(), "sensitive-provider-value") {
		t.Fatalf("opaque provider state leaked through failure evidence: %+v %v", provisionErr.Resources, err)
	}
}

func managedResourceID(resources []compute.ManagedResourceRef) string {
	id, _ := compute.ManagedResourceID(resources, compute.ManagedResourceAccessPolicy)
	return id
}

func TestRunClassifiesLifecycleFailuresByPhase(t *testing.T) {
	cause := errors.New("phase failed")
	tests := []struct {
		name  string
		phase ProvisionPhase
		alter func(*Options)
	}{
		{name: "initialize", phase: ProvisionPhaseInitialize, alter: func(opt *Options) { opt.StatePath = "invalid\x00state" }},
		{name: "tailscale policy", phase: ProvisionPhaseTailscalePolicy, alter: func(opt *Options) { opt.Clients.Tailscale = &fakeTailscale{policyErr: cause} }},
		{name: "cloudflare tunnel", phase: ProvisionPhaseCloudflareTunnel, alter: func(opt *Options) { opt.Clients.Cloudflare = &fakeCloudflare{createErr: cause} }},
		{name: "tailscale auth key", phase: ProvisionPhaseTailscaleAuthKey, alter: func(opt *Options) { opt.Clients.Tailscale = &fakeTailscale{keyErr: cause} }},
		{name: "tailscale policy validation", phase: ProvisionPhaseTailscalePolicyValidation, alter: func(opt *Options) {
			opt.Clients.Tailscale = &validationFailTailscale{fakeTailscale: &fakeTailscale{}, err: cause}
		}},
		{name: "bootstrap render", phase: ProvisionPhaseBootstrapRender, alter: func(opt *Options) { opt.AdminPasswordHash = "invalid" }},
		{name: "compute", phase: ProvisionPhaseCompute, alter: func(opt *Options) { opt.Clients.Compute = &fakeHetzner{serverErr: cause} }},
		{name: "tailscale device", phase: ProvisionPhaseTailscaleDevice, alter: func(opt *Options) {
			opt.Clients.Tailscale = &deviceFailTailscale{fakeTailscale: &fakeTailscale{}, err: cause}
		}},
		{name: "remote ready", phase: ProvisionPhaseRemoteReady, alter: func(opt *Options) { opt.Clients.Remote = &errorRemote{err: errors.New("sudo authentication failure")} }},
		{name: "remote bootstrap", phase: ProvisionPhaseRemoteBootstrap, alter: func(opt *Options) {
			opt.Config.Cloudflare.Tunnel.Enabled = false
			opt.Clients.Remote = &sequenceRemote{errors: []error{nil, cause}}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.ExampleServer("prod", "web")
			cfg.Cloudflare.AccountID = "acc"
			cfg.Cloudflare.Tunnel.Enabled = true
			opt := Options{
				Config: cfg, AdminPasswordHash: testAdminPasswordHash,
				Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: provisionStatePath(t),
				Clients: Clients{Compute: &fakeHetzner{}, Tailscale: &fakeTailscale{}, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}},
			}
			test.alter(&opt)
			_, err := Run(context.Background(), opt)
			var provisionErr *ProvisionError
			if !errors.As(err, &provisionErr) || provisionErr.Phase != test.phase {
				t.Fatalf("expected phase %q typed error, got %T %v", test.phase, err, err)
			}
		})
	}
}

type validationFailTailscale struct {
	*fakeTailscale
	err error
}

func (f *validationFailTailscale) ValidateSSHPolicy(context.Context, []string, string, string) error {
	return f.err
}

type deviceFailTailscale struct {
	*fakeTailscale
	err error
}

func (f *deviceFailTailscale) WaitDevice(context.Context, string, []string) (tailscale.Device, error) {
	return tailscale.Device{}, f.err
}
