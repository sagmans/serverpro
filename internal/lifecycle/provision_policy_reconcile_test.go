package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/state"
)

func TestReconcilePendingTailscalePolicyPromotesConfirmedWrite(t *testing.T) {
	path := provisionStatePath(t)
	st := pendingPolicyState()
	if err := state.Save(path, st); err != nil {
		t.Fatal(err)
	}
	client := &fakeTailscale{policyPresence: tailscale.ServerproPolicyChange{TagOwners: []string{"tag:serverpro-prod"}, SSHRule: true}}

	if err := ReconcilePendingTailscalePolicy(context.Background(), &st, path, client); err != nil {
		t.Fatal(err)
	}
	if tailscalePolicyPending(st.Tailscale) || strings.Join(st.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-prod" || !st.Tailscale.PolicySSHRule {
		t.Fatalf("confirmed write was not promoted: %+v", st.Tailscale)
	}
	persisted, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if tailscalePolicyPending(persisted.Tailscale) || !persisted.Tailscale.PolicySSHRule {
		t.Fatalf("promotion was not durable: %+v", persisted.Tailscale)
	}
}

func TestReconcilePendingTailscalePolicyClearsConfirmedNonWrite(t *testing.T) {
	path := provisionStatePath(t)
	st := pendingPolicyState()
	if err := state.Save(path, st); err != nil {
		t.Fatal(err)
	}

	if err := ReconcilePendingTailscalePolicy(context.Background(), &st, path, &fakeTailscale{}); err != nil {
		t.Fatal(err)
	}
	if tailscalePolicyPending(st.Tailscale) || len(st.Tailscale.PolicyTagOwners) != 0 || st.Tailscale.PolicySSHRule {
		t.Fatalf("confirmed non-write retained ownership: %+v", st.Tailscale)
	}
}

func TestReconcilePendingTailscalePolicyKeepsPartialOrDriftedState(t *testing.T) {
	tests := []struct {
		name   string
		client *fakeTailscale
	}{
		{name: "partial", client: &fakeTailscale{policyPresence: tailscale.ServerproPolicyChange{TagOwners: []string{"tag:serverpro-prod"}}}},
		{name: "drift", client: &fakeTailscale{policyInspectErr: errors.New("ownership drift")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := provisionStatePath(t)
			st := pendingPolicyState()
			if err := state.Save(path, st); err != nil {
				t.Fatal(err)
			}

			err := ReconcilePendingTailscalePolicy(context.Background(), &st, path, test.client)
			if err == nil {
				t.Fatal("expected reconciliation failure")
			}
			if !tailscalePolicyPending(st.Tailscale) {
				t.Fatalf("uncertain ownership was cleared: %+v", st.Tailscale)
			}
			persisted, loadErr := state.Load(path)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if !tailscalePolicyPending(persisted.Tailscale) {
				t.Fatalf("durable uncertain ownership was cleared: %+v", persisted.Tailscale)
			}
		})
	}
}

func TestReconcilePendingTailscalePolicyRestoresMemoryWhenSaveFails(t *testing.T) {
	st := pendingPolicyState()
	err := ReconcilePendingTailscalePolicy(context.Background(), &st, t.TempDir(), &fakeTailscale{policyPresence: tailscale.ServerproPolicyChange{TagOwners: []string{"tag:serverpro-prod"}, SSHRule: true}})
	if err == nil {
		t.Fatal("expected state save failure")
	}
	if !tailscalePolicyPending(st.Tailscale) || len(st.Tailscale.PolicyTagOwners) != 0 || st.Tailscale.PolicySSHRule {
		t.Fatalf("failed save leaked reconciled memory state: %+v", st.Tailscale)
	}
}

func pendingPolicyState() state.State {
	return state.State{Tailscale: state.TailscaleState{
		PolicyPendingTagOwners: []string{"tag:serverpro-prod"},
		PolicyPendingSSHRule:   true,
		PolicySSHTags:          []string{"tag:serverpro-prod"},
		PolicySSHUser:          "deploy",
	}}
}
