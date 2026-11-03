package doctor

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	sshPolicyTestToken = "tok"
	sshPolicyTestAdmin = "deploy"
	sshPolicyTestRoot  = "check-or-disabled"
)

// sshPolicyReader reads rules but cannot write them, like a client that has
// only policy-file read scope. Doctor must then report, never fake a repair.
type sshPolicyReader struct {
	fakeTailscale
	rules []mesh.SSHRule
	err   error
}

func (f sshPolicyReader) SSHPolicy(context.Context) ([]mesh.SSHRule, error) {
	return f.rules, f.err
}

// ensureCall keeps the arguments of one repair, because --fix must ask for
// exactly the rules that config asks for.
type ensureCall struct {
	tags                                    []string
	adminUser, rootPolicy, automationSource string
	// budget is the time left on the repair context; the CLI fixer waits
	// for the tailnet policy lock inside it.
	budget time.Duration
}

// sshPolicyFixer is the full client: it reads rules and records each repair.
type sshPolicyFixer struct {
	sshPolicyReader
	change mesh.PolicyChange
	fixErr error
	calls  []ensureCall
}

func (f *sshPolicyFixer) EnsureServerproPolicy(ctx context.Context, tags []string, adminUser, rootPolicy, automationSource string) (mesh.PolicyChange, error) {
	call := ensureCall{tags: slices.Clone(tags), adminUser: adminUser, rootPolicy: rootPolicy, automationSource: automationSource}
	if deadline, ok := ctx.Deadline(); ok {
		call.budget = time.Until(deadline)
	}
	f.calls = append(f.calls, call)
	return f.change, f.fixErr
}

func sshPolicyConfig(automation bool) config.Config {
	cfg := config.Example("prod")
	cfg.Access.Tailscale.Automation = config.TailscaleAutomation{Enabled: automation}
	return cfg
}

func humanCheckRule(cfg config.Config) mesh.SSHRule {
	return mesh.SSHRule{Action: sshActionCheck, Src: []string{mesh.ServerproPolicyOwner}, Dst: slices.Clone(cfg.Access.Tailscale.Tags), Users: []string{sshPolicyTestAdmin}}
}

func automationAcceptRule(cfg config.Config) mesh.SSHRule {
	return mesh.SSHRule{Action: sshActionAccept, Src: []string{mesh.AutomationSourceTag}, Dst: slices.Clone(cfg.Access.Tailscale.Tags), Users: []string{sshPolicyTestAdmin}}
}

func stateWithSource(source string) state.State {
	return state.State{Tailscale: state.TailscaleState{PolicySSHAutomationSource: source}}
}

// Each finding maps to one way the live rules can drift from config. The
// status split matters: fail blocks unattended runs or leaves access open
// without step-up auth; warn is drift an operator can fix later. The hint
// tells the operator whether --fix, ssh-policy or a hand edit is the way out.
func TestCheckTailnetSSHPolicyFindings(t *testing.T) {
	on, off := sshPolicyConfig(true), sshPolicyConfig(false)
	tags := off.Access.Tailscale.Tags
	for _, tc := range []struct {
		name         string
		cfg          config.Config
		st           state.State
		rules        []mesh.SSHRule
		want         Status
		wantEvidence []string
		wantHint     string
	}{
		{name: "off and clean", cfg: off, rules: []mesh.SSHRule{humanCheckRule(off)}, want: Pass, wantEvidence: []string{"check rule for deploy; automation off"}},
		{name: "on and clean", cfg: on, st: stateWithSource(mesh.AutomationSourceTag), rules: []mesh.SSHRule{humanCheckRule(on), automationAcceptRule(on)}, want: Pass, wantEvidence: []string{"accept from " + mesh.AutomationSourceTag}},
		{name: "human check rule missing", cfg: off, want: Warn, wantEvidence: []string{"human check rule for deploy missing"}, wantHint: tailnetSSHPolicyFixHint},
		{name: "automation accept missing", cfg: on, st: stateWithSource(mesh.AutomationSourceTag), rules: []mesh.SSHRule{humanCheckRule(on)}, want: Fail, wantEvidence: []string{"automation accept rule for deploy missing"}, wantHint: tailnetSSHPolicyFixHint},
		// The accept rule can belong to another server with the same tags and
		// admin user, so doctor only warns and points to the registry-checked
		// revoke in ssh-policy.
		{name: "accept present while off", cfg: off, rules: []mesh.SSHRule{humanCheckRule(off), automationAcceptRule(off)}, want: Warn, wantEvidence: []string{"present but automation is off for this server"}, wantHint: tailnetSSHRevokeHint},
		{
			name:  "root not on check",
			cfg:   off,
			rules: []mesh.SSHRule{humanCheckRule(off), {Action: sshActionAccept, Src: []string{"autogroup:member"}, Dst: tags, Users: []string{sshUserRoot}}},
			want:  Fail, wantEvidence: []string{"root accept rule from [autogroup:member]"}, wantHint: tailnetSSHForeignHint,
		},
		{
			name:  "admin accept from other source",
			cfg:   off,
			rules: []mesh.SSHRule{humanCheckRule(off), {Action: sshActionAccept, Src: []string{"group:sre"}, Dst: tags, Users: []string{sshPolicyTestAdmin}}},
			want:  Warn, wantEvidence: []string{"accept rule for [deploy] from [group:sre], not from " + mesh.AutomationSourceTag}, wantHint: tailnetSSHForeignHint,
		},
		// autogroup:nonroot includes the admin user, so a broad accept rule is
		// the same open door as one that names the admin user.
		{
			name:  "nonroot accept from other source",
			cfg:   off,
			rules: []mesh.SSHRule{humanCheckRule(off), {Action: sshActionAccept, Src: []string{"autogroup:member"}, Dst: tags, Users: []string{sshUserNonRoot}}},
			want:  Warn, wantEvidence: []string{"accept rule for [autogroup:nonroot] from [autogroup:member]"}, wantHint: tailnetSSHForeignHint,
		},
		// A rule from the automation tag in another shape (old tags, another
		// user) is access that no config asks for.
		{
			name:  "other rule from automation tag",
			cfg:   on,
			st:    stateWithSource(mesh.AutomationSourceTag),
			rules: []mesh.SSHRule{humanCheckRule(on), automationAcceptRule(on), {Action: sshActionAccept, Src: []string{mesh.AutomationSourceTag}, Dst: append(slices.Clone(tags), "tag:serverpro-old"), Users: []string{"ops"}}},
			want:  Warn, wantEvidence: []string{"other accept rule from " + mesh.AutomationSourceTag + " for [ops] to [tag:serverpro-prod tag:serverpro-old]"}, wantHint: tailnetSSHForeignHint,
		},
		{name: "state source differs", cfg: on, rules: []mesh.SSHRule{humanCheckRule(on), automationAcceptRule(on)}, want: Warn, wantEvidence: []string{`state records automation source "", config asks for "` + mesh.AutomationSourceTag + `"`}, wantHint: tailnetSSHPolicyFixHint},
		{
			name:  "rules for other servers ignored",
			cfg:   off,
			rules: []mesh.SSHRule{humanCheckRule(off), {Action: sshActionAccept, Src: []string{mesh.AutomationSourceTag}, Dst: []string{"tag:other"}, Users: []string{sshUserRoot, sshPolicyTestAdmin}}},
			want:  Pass, wantEvidence: []string{"automation off"},
		},
		{
			name:  "several findings joined",
			cfg:   on,
			rules: []mesh.SSHRule{{Action: sshActionCheck, Src: []string{"group:sre"}, Dst: tags, Users: []string{sshUserRoot}}},
			want:  Fail, wantEvidence: []string{"human check rule for deploy missing; automation accept rule for deploy missing", "state records automation source"}, wantHint: tailnetSSHPolicyFixHint,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: tc.rules}}
			result := checkTailnetSSHPolicy(context.Background(), tc.cfg, tc.st, sshPolicyTestToken, client, false)
			if result.Name != tailnetSSHPolicyCheckName || result.Scope != "provider" || result.Status != tc.want {
				t.Fatalf("result = %+v, want %s", result, tc.want)
			}
			for _, want := range tc.wantEvidence {
				if !strings.Contains(result.Evidence, want) {
					t.Fatalf("evidence %q missing %q", result.Evidence, want)
				}
			}
			if result.Remediation != tc.wantHint {
				t.Fatalf("remediation = %q, want %q", result.Remediation, tc.wantHint)
			}
			if len(client.calls) != 0 {
				t.Fatalf("check without --fix wrote policy: calls=%+v", client.calls)
			}
		})
	}
}

// Rule matching is set-based: the admin console may reorder a multi-tag
// destination list, and that must not look like a missing rule.
func TestCheckTailnetSSHPolicyMatchesRulesAsSets(t *testing.T) {
	cfg := sshPolicyConfig(true)
	cfg.Access.Tailscale.Tags = []string{"tag:serverpro-prod", "tag:serverpro-web"}
	check, accept := humanCheckRule(cfg), automationAcceptRule(cfg)
	slices.Reverse(check.Dst)
	slices.Reverse(accept.Dst)
	client := sshPolicyReader{rules: []mesh.SSHRule{check, accept}}
	if result := checkTailnetSSHPolicy(context.Background(), cfg, stateWithSource(mesh.AutomationSourceTag), sshPolicyTestToken, client, false); result.Status != Pass {
		t.Fatalf("result = %+v", result)
	}
}

// The check must stay optional: no token or a client without policy access
// is a skip, and a read failure is a warn with a scope hint, never a fail
// that blocks the rest of doctor.
func TestCheckTailnetSSHPolicyWithoutPolicyAccess(t *testing.T) {
	cfg := sshPolicyConfig(true)
	for _, tc := range []struct {
		name         string
		token        string
		client       TailscaleClient
		want         Status
		wantEvidence string
	}{
		{name: "no token", client: sshPolicyReader{}, want: Skip, wantEvidence: "no API token"},
		{name: "client without policy", token: sshPolicyTestToken, client: fakeTailscale{}, want: Skip, wantEvidence: "not supported"},
		{name: "snapshot of client without policy", token: sshPolicyTestToken, client: snapshotProviderClients(Clients{Tailscale: fakeTailscale{}}).Tailscale, want: Skip, wantEvidence: "not supported"},
		{name: "fetch error", token: sshPolicyTestToken, client: sshPolicyReader{err: errors.New("403 forbidden")}, want: Warn, wantEvidence: "policy fetch failed: 403 forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := checkTailnetSSHPolicy(context.Background(), cfg, state.State{}, tc.token, tc.client, true)
			if result.Status != tc.want || !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v, want %s with %q", result, tc.want, tc.wantEvidence)
			}
			if tc.want == Warn && !strings.Contains(result.Remediation, "policy-file read access") {
				t.Fatalf("remediation = %q", result.Remediation)
			}
		})
	}
}

// --fix uses the provision write with config's intent and reports what it
// fixed. The CLI fixer behind this call takes the lock and records state, so
// doctor must hand it exactly the rules that config asks for, with a budget
// that covers waiting for that lock.
func TestCheckTailnetSSHPolicyFixRepairsFixableFindings(t *testing.T) {
	cfg := sshPolicyConfig(true)
	client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: []mesh.SSHRule{humanCheckRule(cfg)}}, change: mesh.PolicyChange{AutomationSSHRule: true}}

	result := checkTailnetSSHPolicy(context.Background(), cfg, stateWithSource(mesh.AutomationSourceTag), sshPolicyTestToken, client, true)
	if result.Status != Pass || !strings.HasPrefix(result.Evidence, "fixed: automation accept rule for deploy missing") || !strings.HasSuffix(result.Evidence, "now check rule for deploy; accept from "+mesh.AutomationSourceTag) {
		t.Fatalf("result = %+v", result)
	}
	if len(client.calls) != 1 {
		t.Fatalf("calls = %+v", client.calls)
	}
	got := client.calls[0]
	if !slices.Equal(got.tags, cfg.Access.Tailscale.Tags) || got.adminUser != sshPolicyTestAdmin || got.rootPolicy != sshPolicyTestRoot || got.automationSource != mesh.AutomationSourceTag {
		t.Fatalf("repair args = %+v", got)
	}
	if got.budget <= tailnetSSHPolicyTimeout || got.budget > tailnetSSHPolicyFixTimeout {
		t.Fatalf("repair deadline budget = %s, want above %s and at most %s", got.budget, tailnetSSHPolicyTimeout, tailnetSSHPolicyFixTimeout)
	}
}

// Doctor never revokes the shared accept rule. With only that finding there
// is nothing --fix may do, so it must not write and must point to ssh-policy.
func TestCheckTailnetSSHPolicyFixLeavesSharedAcceptRule(t *testing.T) {
	cfg := sshPolicyConfig(false)
	client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: []mesh.SSHRule{humanCheckRule(cfg), automationAcceptRule(cfg)}}}

	result := checkTailnetSSHPolicy(context.Background(), cfg, state.State{}, sshPolicyTestToken, client, true)
	if result.Status != Warn || result.Remediation != tailnetSSHRevokeHint || len(client.calls) != 0 {
		t.Fatalf("result=%+v calls=%+v", result, client.calls)
	}
}

// When fixable and unfixable findings mix, the fixable ones are repaired and
// the rest stay in the result with their own hint, so the check still shows
// what the operator must do.
func TestCheckTailnetSSHPolicyFixKeepsUnfixableFindings(t *testing.T) {
	cfg := sshPolicyConfig(false)
	rootAccept := mesh.SSHRule{Action: sshActionAccept, Src: []string{"autogroup:member"}, Dst: cfg.Access.Tailscale.Tags, Users: []string{sshUserRoot}}
	for _, tc := range []struct {
		name         string
		rules        []mesh.SSHRule
		st           state.State
		want         Status
		wantEvidence string
		wantHint     string
	}{
		{name: "root accept", rules: []mesh.SSHRule{rootAccept}, want: Fail, wantEvidence: "fixed: human check rule for deploy missing; remaining: root accept rule from [autogroup:member]", wantHint: tailnetSSHForeignHint},
		{
			name: "shared accept rule", rules: []mesh.SSHRule{humanCheckRule(cfg), automationAcceptRule(cfg)}, st: stateWithSource(mesh.AutomationSourceTag), want: Warn,
			wantEvidence: `fixed: state records automation source "` + mesh.AutomationSourceTag + `", config asks for ""; remaining: automation accept rule for deploy present`, wantHint: tailnetSSHRevokeHint,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: tc.rules}}
			result := checkTailnetSSHPolicy(context.Background(), cfg, tc.st, sshPolicyTestToken, client, true)
			if result.Status != tc.want || result.Remediation != tc.wantHint || !strings.HasPrefix(result.Evidence, tc.wantEvidence) {
				t.Fatalf("result = %+v", result)
			}
			if len(client.calls) != 1 || client.calls[0].automationSource != "" {
				t.Fatalf("calls = %+v", client.calls)
			}
		})
	}
}

// With only foreign findings there is nothing --fix may touch, so it must not
// call the write at all.
func TestCheckTailnetSSHPolicyFixSkipsWriteForForeignOnly(t *testing.T) {
	cfg := sshPolicyConfig(false)
	foreign := mesh.SSHRule{Action: sshActionAccept, Src: []string{"group:sre"}, Dst: cfg.Access.Tailscale.Tags, Users: []string{sshPolicyTestAdmin}}
	client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: []mesh.SSHRule{humanCheckRule(cfg), foreign}}}

	result := checkTailnetSSHPolicy(context.Background(), cfg, state.State{}, sshPolicyTestToken, client, true)
	if result.Status != Warn || result.Remediation != tailnetSSHForeignHint || len(client.calls) != 0 {
		t.Fatalf("result=%+v calls=%+v", result, client.calls)
	}
}

// A failed or impossible repair keeps the original findings visible, so the
// operator sees what is still wrong.
func TestCheckTailnetSSHPolicyFixFailuresKeepFindings(t *testing.T) {
	cfg := sshPolicyConfig(true)
	rules := []mesh.SSHRule{humanCheckRule(cfg)}
	st := stateWithSource(mesh.AutomationSourceTag)
	for _, tc := range []struct {
		name         string
		client       TailscaleClient
		wantEvidence string
	}{
		{name: "write error", client: &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: rules}, fixErr: errors.New("412 precondition failed")}, wantEvidence: "repair failed: 412 precondition failed; findings: automation accept rule for deploy missing"},
		{name: "read-only client", client: sshPolicyReader{rules: rules}, wantEvidence: "automation accept rule for deploy missing"},
		{name: "snapshot of read-only client", client: snapshotProviderClients(Clients{Tailscale: sshPolicyReader{rules: rules}}).Tailscale, wantEvidence: "repair failed: " + errTailnetSSHPolicyUnsupported.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := checkTailnetSSHPolicy(context.Background(), cfg, st, sshPolicyTestToken, tc.client, true)
			if result.Status != Fail || !strings.Contains(result.Evidence, tc.wantEvidence) || result.Remediation != tailnetSSHPolicyFixHint {
				t.Fatalf("result=%+v, want fail with %q", result, tc.wantEvidence)
			}
		})
	}
}

// A clean policy under --fix must not write, because every write is a
// tailnet-wide change.
func TestCheckTailnetSSHPolicyFixNoopsWhenClean(t *testing.T) {
	cfg := sshPolicyConfig(true)
	client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: []mesh.SSHRule{humanCheckRule(cfg), automationAcceptRule(cfg)}}}
	result := checkTailnetSSHPolicy(context.Background(), cfg, stateWithSource(mesh.AutomationSourceTag), sshPolicyTestToken, client, true)
	if result.Status != Pass || len(client.calls) != 0 {
		t.Fatalf("result=%+v calls=%+v", result, client.calls)
	}
}

// The full doctor run wraps clients in snapshots; the repair must pass
// through that wrapper to the fixer the CLI gave doctor, and its result must
// reach the report.
func TestRunWithOptionsPassesSSHPolicyRepairThroughSnapshot(t *testing.T) {
	cfg := sshPolicyConfig(true)
	cfg.Cloudflare.AccountID = "acc"
	client := &sshPolicyFixer{sshPolicyReader: sshPolicyReader{rules: []mesh.SSHRule{humanCheckRule(cfg)}}, change: mesh.PolicyChange{AutomationSSHRule: true}}
	st := doctorState(cfg, "192.0.2.10", "")
	st.Tailscale.PolicySSHAutomationSource = mesh.AutomationSourceTag

	report := RunWithOptions(context.Background(), cfg, st, credentials.Set{Tailscale: "ts-token-long"}, Clients{Compute: fakeCompute{}, Tailscale: client, PublicSSHProbe: refusedPublicSSHProbe}, Options{Fix: true})
	if !hasResult(report, tailnetSSHPolicyCheckName, Pass, "fixed: automation accept rule") || len(client.calls) != 1 {
		t.Fatalf("results=%+v calls=%+v", report.Results, client.calls)
	}
}
