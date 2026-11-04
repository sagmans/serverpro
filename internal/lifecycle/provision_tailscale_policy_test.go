package lifecycle

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/state"
)

const policyTestStatePath = "/state/prod/web.json"

// exactPolicyTailscale returns the policy change it is given, also an empty
// one. The shared fakeTailscale turns an empty change into a full write, so it
// cannot model a policy that already matches config.
type exactPolicyTailscale struct {
	*fakeTailscale
	change mesh.PolicyChange
	err    error
	source []string
}

func (f *exactPolicyTailscale) EnsureServerproPolicy(_ context.Context, _, _ []string, _, _, automationSource, _ string) (mesh.PolicyChange, error) {
	f.calls = append(f.calls, "ensure-policy")
	f.source = append(f.source, automationSource)
	return f.change, f.err
}

// policySaves records each state save, because the save is what makes a
// policy write visible to reconcile, doctor and delete.
type policySaves struct {
	paths  []string
	states []state.State
}

func (s *policySaves) save(path string, st state.State) error {
	s.paths = append(s.paths, path)
	s.states = append(s.states, st)
	return nil
}

func policyTestConfig(automation bool) config.Config {
	cfg := config.Example("prod")
	cfg.Access.Tailscale.Automation = config.TailscaleAutomation{Enabled: automation}
	return cfg
}

// The automation tag owner is protected by the recorded source, not by
// PolicyTagOwners; keeping it out lets reconcile prune it once no server
// uses automation. The server tag owner is still recorded for delete.
func TestRecordTailscalePolicyChangeKeepsAutomationTagOutOfOwners(t *testing.T) {
	cfg := policyTestConfig(true)
	var st state.State
	RecordTailscalePolicyChange(&st, cfg, mesh.PolicyChange{TagOwners: []string{"tag:serverpro-prod", mesh.AutomationSourceTag}, SSHRule: true, AutomationSSHRule: true})
	if !slices.Equal(st.Tailscale.PolicyTagOwners, []string{"tag:serverpro-prod"}) {
		t.Fatalf("tag owners = %v", st.Tailscale.PolicyTagOwners)
	}
	if !st.Tailscale.PolicySSHRule || !slices.Equal(st.Tailscale.PolicySSHTags, cfg.Access.Tailscale.Tags) || st.Tailscale.PolicySSHAutomationSource != mesh.AutomationSourceTag {
		t.Fatalf("state = %+v", st.Tailscale)
	}
}

// The source follows config after every successful write, even an empty
// one: a hand-made rule or one another server wrote must still be recorded,
// and turning automation off must clear the record.
func TestRecordTailscalePolicyChangeFollowsConfigWithoutChange(t *testing.T) {
	var on state.State
	RecordTailscalePolicyChange(&on, policyTestConfig(true), mesh.PolicyChange{})
	if on.Tailscale.PolicySSHAutomationSource != mesh.AutomationSourceTag || !slices.Equal(on.Tailscale.PolicySSHTags, []string{"tag:serverpro-prod"}) || on.Tailscale.PolicySSHRule {
		t.Fatalf("automation on: %+v", on.Tailscale)
	}

	kept := state.State{Tailscale: state.TailscaleState{PolicySSHTags: []string{"tag:serverpro-old"}}}
	RecordTailscalePolicyChange(&kept, policyTestConfig(true), mesh.PolicyChange{})
	if !slices.Equal(kept.Tailscale.PolicySSHTags, []string{"tag:serverpro-old"}) {
		t.Fatalf("recorded tags must not be replaced: %v", kept.Tailscale.PolicySSHTags)
	}

	off := state.State{Tailscale: state.TailscaleState{PolicySSHAutomationSource: mesh.AutomationSourceTag}}
	RecordTailscalePolicyChange(&off, policyTestConfig(false), mesh.PolicyChange{})
	if off.Tailscale.PolicySSHAutomationSource != "" || len(off.Tailscale.PolicySSHTags) != 0 {
		t.Fatalf("automation off: %+v", off.Tailscale)
	}
}

// Provision saves state only when the record moves, so a rerun against a
// matching policy does not rewrite the state file, while a changed source or
// newly filled tags are never lost.
func TestEnsureTailscalePolicySavesOnlyWhenRecordChanges(t *testing.T) {
	creds := credentials.Set{Tailscale: testTailscaleAPIToken}
	for _, tc := range []struct {
		name      string
		cfg       config.Config
		before    state.TailscaleState
		change    mesh.PolicyChange
		wantSaves int
	}{
		{name: "policy write", cfg: policyTestConfig(false), change: mesh.PolicyChange{SSHRule: true}, wantSaves: 1},
		{name: "matching policy and record", cfg: policyTestConfig(false), before: state.TailscaleState{PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-prod"}}},
		{name: "source turned on", cfg: policyTestConfig(true), before: state.TailscaleState{PolicySSHTags: []string{"tag:serverpro-prod"}}, wantSaves: 1},
		{name: "source turned off", cfg: policyTestConfig(false), before: state.TailscaleState{PolicySSHTags: []string{"tag:serverpro-prod"}, PolicySSHAutomationSource: mesh.AutomationSourceTag}, wantSaves: 1},
		{name: "matching automation record", cfg: policyTestConfig(true), before: state.TailscaleState{PolicySSHTags: []string{"tag:serverpro-prod"}, PolicySSHAutomationSource: mesh.AutomationSourceTag}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &exactPolicyTailscale{fakeTailscale: &fakeTailscale{}, change: tc.change}
			st := state.State{Tailscale: tc.before}
			saves := &policySaves{}
			if err := ensureTailscalePolicy(context.Background(), &st, policyTestStatePath, client, creds, tc.cfg, saves.save); err != nil {
				t.Fatal(err)
			}
			if len(saves.paths) != tc.wantSaves {
				t.Fatalf("saves = %d, want %d (state %+v)", len(saves.paths), tc.wantSaves, st.Tailscale)
			}
			if tc.wantSaves > 0 && (saves.paths[0] != policyTestStatePath || saves.states[0].Tailscale.PolicySSHAutomationSource != tc.cfg.Access.Tailscale.AutomationSource()) {
				t.Fatalf("saved %q %+v", saves.paths[0], saves.states[0].Tailscale)
			}
			if !slices.Equal(client.source, []string{tc.cfg.Access.Tailscale.AutomationSource()}) {
				t.Fatalf("automation source asked = %v", client.source)
			}
		})
	}
}

// Without an API token provision runs in auth-key mode and must not touch
// the policy; a failed write must not be recorded as applied.
func TestEnsureTailscalePolicySkipsOrFailsWithoutRecord(t *testing.T) {
	cfg := policyTestConfig(true)
	client := &exactPolicyTailscale{fakeTailscale: &fakeTailscale{}}
	saves := &policySaves{}
	var st state.State
	if err := ensureTailscalePolicy(context.Background(), &st, policyTestStatePath, client, credentials.Set{}, cfg, saves.save); err != nil || len(client.calls) != 0 || len(saves.paths) != 0 {
		t.Fatalf("no token: err=%v calls=%v saves=%v", err, client.calls, saves.paths)
	}

	writeErr := errors.New("412 precondition failed")
	client.err = writeErr
	if err := ensureTailscalePolicy(context.Background(), &st, policyTestStatePath, client, credentials.Set{Tailscale: testTailscaleAPIToken}, cfg, saves.save); !errors.Is(err, writeErr) {
		t.Fatalf("err = %v", err)
	}
	if len(saves.paths) != 0 || st.Tailscale.PolicySSHAutomationSource != "" {
		t.Fatalf("failed write recorded: saves=%v state=%+v", saves.paths, st.Tailscale)
	}
}

// A server that already owns its check rule records the rule tags of the
// current config after a successful write, also when the write changed
// nothing because the rule for ssh_tags was moved there by hand.
func TestRecordTailscalePolicyChangeFollowsSSHTags(t *testing.T) {
	cfg := policyTestConfig(true)
	cfg.Access.Tailscale.Tags = []string{"tag:serverpro-prod", "tag:serverpro-prod-webapp"}
	cfg.Access.Tailscale.SSHTags = []string{"tag:serverpro-prod-webapp"}
	owned := state.State{Tailscale: state.TailscaleState{PolicySSHRule: true, PolicySSHTags: []string{"tag:serverpro-prod", "tag:serverpro-prod-webapp"}}}
	RecordTailscalePolicyChange(&owned, cfg, mesh.PolicyChange{})
	if !slices.Equal(owned.Tailscale.PolicySSHTags, []string{"tag:serverpro-prod-webapp"}) {
		t.Fatalf("owned rule tags = %v", owned.Tailscale.PolicySSHTags)
	}
}
