package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/state"
)

func TestRunEnsuresTailscalePolicyBeforeAuthKeyAndTracksManagedPolicy(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	ts := &fakeTailscale{}
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: provisionStatePath(t), Clients: Clients{Compute: &fakeHetzner{}, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: &fakeRemote{}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ts.calls, ","); !strings.HasPrefix(got, "tailnet-id,snapshot-devices,ensure-policy,create-key") {
		t.Fatalf("tailscale calls = %s", got)
	}
	if !st.Tailscale.PolicySSHRule || strings.Join(st.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-prod" || strings.Join(st.Tailscale.PolicySSHTags, ",") != "tag:serverpro-prod" || st.Tailscale.PolicySSHUser != "deploy" || tailscalePolicyPending(st.Tailscale) {
		t.Fatalf("managed policy state missing: %+v", st.Tailscale)
	}
}

func TestEnsureTailscalePolicyTracksReusedSSHRuleIdentity(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Project: "prod", Server: cfg.Server}
	statePath := provisionStatePath(t)

	if err := ensureTailscalePolicy(context.Background(), &st, statePath, &fakeTailscale{policyUnchanged: true}, credentials.Set{Tailscale: "ts-api"}, cfg); err != nil {
		t.Fatal(err)
	}
	if st.Tailscale.PolicySSHRule || strings.Join(st.Tailscale.PolicySSHTags, ",") != "tag:serverpro-prod" || st.Tailscale.PolicySSHUser != "deploy" {
		t.Fatalf("reused policy identity missing: %+v", st.Tailscale)
	}
	persisted, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(persisted.Tailscale.PolicySSHTags, ",") != "tag:serverpro-prod" || persisted.Tailscale.PolicySSHUser != "deploy" {
		t.Fatalf("reused policy identity was not checkpointed: %+v", persisted.Tailscale)
	}
}

func TestEnsureTailscalePolicyRejectsUncertainOwnedRuleIdentity(t *testing.T) {
	baseConfig := config.Example("prod")
	for _, tc := range []struct {
		name  string
		state state.TailscaleState
		edit  func(*config.Config)
	}{
		{name: "legacy user missing", state: state.TailscaleState{PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-prod"}}},
		{name: "user drift", state: state.TailscaleState{PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-prod"}, PolicySSHUser: "deploy"}, edit: func(cfg *config.Config) { cfg.Admin.Username = "operator" }},
		{name: "tag drift", state: state.TailscaleState{PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-prod"}, PolicySSHUser: "deploy"}, edit: func(cfg *config.Config) { cfg.Access.Tailscale.Tags = []string{"tag:changed"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig
			if tc.edit != nil {
				tc.edit(&cfg)
			}
			st := state.State{Project: "prod", Server: cfg.Server, Tailscale: tc.state}
			client := &fakeTailscale{}

			err := ensureTailscalePolicy(context.Background(), &st, provisionStatePath(t), client, credentials.Set{Tailscale: "ts-api"}, cfg)
			if err == nil || !strings.Contains(err.Error(), "tracked Tailscale SSH policy identity") {
				t.Fatalf("expected identity guard, got %v", err)
			}
			if len(client.calls) != 0 {
				t.Fatalf("identity guard ran after provider mutation: %v", client.calls)
			}
		})
	}
}

func TestEnsureTailscalePolicyStopsBeforeMutationWhenCheckpointFails(t *testing.T) {
	cfg := config.Example("prod")
	st := state.State{Project: "prod", Server: cfg.Server}
	client := &fakeTailscale{}

	err := ensureTailscalePolicy(context.Background(), &st, t.TempDir(), client, credentials.Set{Tailscale: "ts-api"}, cfg)
	if err == nil {
		t.Fatal("expected state checkpoint failure")
	}
	if client.policyMutated {
		t.Fatal("provider policy mutated before state checkpoint")
	}
	if tailscalePolicyPending(st.Tailscale) {
		t.Fatalf("failed checkpoint leaked in-memory pending ownership: %+v", st.Tailscale)
	}
}

func TestEnsureTailscalePolicyClearsPendingOnPreconditionFailure(t *testing.T) {
	cfg := config.Example("prod")
	statePath := provisionStatePath(t)
	st := state.State{Project: "prod", Server: cfg.Server}
	client := &fakeTailscale{policyPostErr: &httpjson.StatusError{StatusCode: http.StatusPreconditionFailed, Status: "412 Precondition Failed"}}

	err := ensureTailscalePolicy(context.Background(), &st, statePath, client, credentials.Set{Tailscale: "ts-api"}, cfg)
	if err == nil || !httpjson.IsStatus(err, http.StatusPreconditionFailed) {
		t.Fatalf("expected policy precondition error, got %v", err)
	}
	if tailscalePolicyPending(st.Tailscale) || len(st.Tailscale.PolicyTagOwners) != 0 || st.Tailscale.PolicySSHRule {
		t.Fatalf("definite non-write retained ownership: %+v", st.Tailscale)
	}
	persisted, loadErr := state.Load(statePath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if tailscalePolicyPending(persisted.Tailscale) || len(persisted.Tailscale.PolicyTagOwners) != 0 || persisted.Tailscale.PolicySSHRule {
		t.Fatalf("persisted definite non-write retained ownership: %+v", persisted.Tailscale)
	}
}

func TestEnsureTailscalePolicyRetainsPendingOnAmbiguousFailureAndReconcilesRetry(t *testing.T) {
	cfg := config.Example("prod")
	statePath := provisionStatePath(t)
	st := state.State{Project: "prod", Server: cfg.Server}
	client := &fakeTailscale{policyPostErr: errors.New("policy response lost")}

	err := ensureTailscalePolicy(context.Background(), &st, statePath, client, credentials.Set{Tailscale: "ts-api"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "response lost") {
		t.Fatalf("expected ambiguous policy error, got %v", err)
	}
	if strings.Join(st.Tailscale.PolicyPendingTagOwners, ",") != "tag:serverpro-prod" || !st.Tailscale.PolicyPendingSSHRule || len(st.Tailscale.PolicyTagOwners) != 0 || st.Tailscale.PolicySSHRule {
		t.Fatalf("ambiguous write did not retain only pending ownership: %+v", st.Tailscale)
	}
	persisted, loadErr := state.Load(statePath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !tailscalePolicyPending(persisted.Tailscale) {
		t.Fatalf("pending ownership was not durable: %+v", persisted.Tailscale)
	}

	retry := &fakeTailscale{}
	if err := ensureTailscalePolicy(context.Background(), &st, statePath, retry, credentials.Set{Tailscale: "ts-api"}, cfg); err != nil {
		t.Fatalf("reconcile pending ownership: %v", err)
	}
	if got := strings.Join(retry.calls, ","); got != "inspect-policy,ensure-policy" {
		t.Fatalf("retry calls = %s", got)
	}
	if tailscalePolicyPending(st.Tailscale) || strings.Join(st.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-prod" || !st.Tailscale.PolicySSHRule {
		t.Fatalf("retry did not confirm ownership: %+v", st.Tailscale)
	}
}

func TestRunCreatesJITAuthKeyAndNeverStoresSecrets(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Cloudflare.Tunnel.Enabled = true
	h := &fakeHetzner{}
	ts := &fakeTailscale{}
	r := &fakeRemote{}
	statePath := provisionStatePath(t)
	st, err := Run(context.Background(), Options{Config: cfg, AdminPasswordHash: testAdminPasswordHash, Creds: credentials.Set{Tailscale: "ts-api", Cloudflare: "cf"}, StatePath: statePath, Clients: Clients{Compute: h, Tailscale: ts, Cloudflare: &fakeCloudflare{}, Remote: r}})
	if err != nil {
		t.Fatal(err)
	}
	if !ts.created || !strings.Contains(strings.Join(ts.calls, ","), "delete-key") {
		t.Fatalf("expected JIT auth key create/delete calls, got %v", ts.calls)
	}
	if !strings.Contains(h.userData, "tskey-auth-created") {
		t.Fatal("cloud-init missing JIT auth key")
	}
	if !strings.Contains(h.userData, testAdminPasswordHash) || strings.Contains(h.userData, "NOPASSWD") {
		t.Fatalf("cloud-init missing password-required sudo state:\n%s", h.userData)
	}
	if strings.Contains(h.userData, "ts-api") || strings.Contains(h.userData, "cf") {
		t.Fatal("cloud-init leaked provider token")
	}
	if st.Compute.ID != "2" || st.Tailscale.NodeID != "d1" || st.Tailscale.AuthKeyID != "" || st.Cloudflare.TunnelID != "tun1" {
		t.Fatalf("bad state: %+v", st)
	}
	if strings.Contains(st.Project, "tskey") {
		t.Fatal("state leaked auth key")
	}
	saved, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tailscale.AuthKeyID != "" {
		t.Fatalf("saved state retained auth key id: %+v", saved.Tailscale)
	}
}
