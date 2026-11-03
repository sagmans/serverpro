package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	sshPolicyToken   = "ts"
	sshPolicySocket  = "/var/run/serverpro-automation/tailscaled.sock"
	sshPolicyCLI     = "/opt/homebrew/bin/tailscale"
	sshPolicyNSTag   = "tag:serverpro-demo"
	sshPolicyAdmin   = "deploy"
	sshPolicyRoot    = "check-or-disabled"
	sshPolicyTailnet = "example.ts.net"
	// sshPolicyLockProbe bounds the check that the tailnet lock is held; a
	// held lock makes the probe time out instead of hanging the test.
	sshPolicyLockProbe = 50 * time.Millisecond
)

// fakeSSHPolicyClient stands in for the Tailscale adapter through the
// serverSSHPolicyClient hook. It records the call order and, at each write,
// whether the tailnet policy lock is held and what state says, because the
// ordering (lock, write, record, revoke, validate) is the safety property.
type fakeSSHPolicyClient struct {
	t            *testing.T
	stPath       string
	ensureChange mesh.PolicyChange
	ensureErr    error
	removeChange mesh.PolicyChange
	validateErr  error

	calls          []string
	ensureSources  []string
	lockHeld       []bool
	sourceAtRemove []string
}

func (f *fakeSSHPolicyClient) EnsureServerproPolicy(ctx context.Context, tags []string, adminUser, rootPolicy, automationSource string) (mesh.PolicyChange, error) {
	f.calls = append(f.calls, "ensure")
	f.ensureSources = append(f.ensureSources, automationSource)
	if !slices.Equal(tags, []string{sshPolicyNSTag}) || adminUser != sshPolicyAdmin || rootPolicy != sshPolicyRoot {
		f.t.Errorf("ensure args tags=%v admin=%q root=%q", tags, adminUser, rootPolicy)
	}
	f.lockHeld = append(f.lockHeld, tailnetLockHeld(f.t))
	return f.ensureChange, f.ensureErr
}

func (f *fakeSSHPolicyClient) RemoveAutomationSSHRule(_ context.Context, tags []string, adminUser string) (mesh.PolicyChange, error) {
	f.calls = append(f.calls, "remove")
	if !slices.Equal(tags, []string{sshPolicyNSTag}) || adminUser != sshPolicyAdmin {
		f.t.Errorf("remove args tags=%v admin=%q", tags, adminUser)
	}
	f.sourceAtRemove = append(f.sourceAtRemove, loadSSHPolicyStateAt(f.t, f.stPath).PolicySSHAutomationSource)
	return f.removeChange, nil
}

func (f *fakeSSHPolicyClient) ValidateSSHPolicy(context.Context, []string, string, string, string) error {
	f.calls = append(f.calls, "validate")
	return f.validateErr
}

// tailnetLockHeld probes the tailnet policy lock from a second file
// descriptor; flock makes the probe wait, so a timeout means it is held.
func tailnetLockHeld(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), sshPolicyLockProbe)
	defer cancel()
	unlock, err := state.LockTailnetPolicy(ctx, config.RegistryPath(), config.TokenDefaultTailnet)
	if err != nil {
		return true
	}
	unlock()
	return false
}

func sshPolicyServerConfig(namespace, server string, automation bool) config.Config {
	cfg := config.ExampleServer(namespace, server)
	cfg.Cloudflare.AccountID = "acc"
	if automation {
		cfg.Access.Tailscale.Automation = config.TailscaleAutomation{Enabled: true, Socket: sshPolicySocket, CLI: sshPolicyCLI}
	}
	return cfg
}

// registerSSHPolicyServer stores one managed server (config, state, registry
// entry) the way create leaves it, so the sharer scan reads real files.
func registerSSHPolicyServer(t *testing.T, cfg config.Config, ts state.TailscaleState) {
	t.Helper()
	server := targetServer(cfg.Server)
	if err := config.Save(config.ServerConfigPath(cfg.Namespace, server), cfg); err != nil {
		t.Fatal(err)
	}
	ts.Name = cfg.Namespace + "-" + server
	st := state.State{Namespace: cfg.Namespace, Server: server, Compute: state.ComputeState{Provider: "hetzner", Account: "prod", Namespace: cfg.Namespace, Server: server, ID: "42", Name: ts.Name}, Tailscale: ts}
	registerSSHPolicyEntry(t, st, config.ServerConfigPath(cfg.Namespace, server))
}

func registerSSHPolicyEntry(t *testing.T, st state.State, configPath string) {
	t.Helper()
	stPath := config.ServerStatePath(st.Namespace, st.Server)
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: st.Namespace, Server: st.Server, StatePath: stPath, ConfigPath: configPath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// writeSSHPolicyFixture creates the target server demo/web in a temp HOME
// with its credentials, so ssh-policy runs its real config, credential, lock
// and state code against the fake client.
func writeSSHPolicyFixture(t *testing.T, automation bool, ts state.TailscaleState) (config.Config, string) {
	t.Helper()
	createTestHome(t)
	cfg := sshPolicyServerConfig("demo", "web", automation)
	ts.Tailnet = sshPolicyTailnet
	registerSSHPolicyServer(t, cfg, ts)
	if err := credentials.Save(cfg, credentials.Set{Namespace: "demo", Server: "web", ServerProvider: "acct", Tailscale: sshPolicyToken, Cloudflare: "cf"}); err != nil {
		t.Fatal(err)
	}
	return cfg, config.ServerStatePath("demo", "web")
}

// sshPolicyApp wires the fake client through the service hook and counts
// constructions, so a test can prove that no client was built at all.
func sshPolicyApp(out io.Writer, dryRun bool, client *fakeSSHPolicyClient, built *[]string) *app {
	return &app{project: "demo", nonInteractive: true, dryRun: dryRun, stdout: out, stderr: io.Discard, services: serviceHooks{
		serverSSHPolicyClient: func(token, tailnet string) serverSSHPolicyClient {
			*built = append(*built, token+"@"+tailnet)
			return client
		},
	}}
}

func runSSHPolicyCommand(t *testing.T, dryRun bool, client *fakeSSHPolicyClient) (serverSSHPolicyRow, []string, error) {
	t.Helper()
	var out bytes.Buffer
	var built []string
	cmd := sshPolicyApp(&out, dryRun, client, &built).serverSSHPolicyCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"web"})
	err := cmd.Execute()
	var row serverSSHPolicyRow
	if err == nil {
		if decodeErr := json.Unmarshal(out.Bytes(), &row); decodeErr != nil {
			t.Fatalf("output=%s err=%v", out.String(), decodeErr)
		}
	} else if out.Len() != 0 {
		t.Fatalf("failed run printed a row: %s", out.String())
	}
	return row, built, err
}

func loadSSHPolicyStateAt(t *testing.T, stPath string) state.TailscaleState {
	t.Helper()
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	return st.Tailscale
}

// A dry run is the operator's preview of a tailnet-wide change, so it must
// report the intended rules without building a client or writing state.
func TestServerSSHPolicyDryRunPlansWithoutClient(t *testing.T) {
	_, stPath := writeSSHPolicyFixture(t, true, state.TailscaleState{})
	client := &fakeSSHPolicyClient{t: t, stPath: stPath}

	row, built, err := runSSHPolicyCommand(t, true, client)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "planned" || !row.DryRun || row.Namespace != "demo" || row.Server != "web" || row.AdminUser != sshPolicyAdmin || row.AutomationSource != mesh.AutomationSourceTag || !slices.Equal(row.Tags, []string{sshPolicyNSTag}) {
		t.Fatalf("row = %+v", row)
	}
	if len(built) != 0 || len(client.calls) != 0 {
		t.Fatalf("dry run built=%v calls=%v", built, client.calls)
	}
	if got := loadSSHPolicyStateAt(t, stPath); got.PolicySSHAutomationSource != "" || got.PolicySSHRule {
		t.Fatalf("dry run wrote state: %+v", got)
	}
}

// Turning automation on is the main use: the write runs under the tailnet
// lock with the stored token, state records the source before the read-back,
// and the automation tag owner stays out of PolicyTagOwners so reconcile can
// prune it once no server records the source.
func TestServerSSHPolicyAppliesAutomationRuleAndRecordsState(t *testing.T) {
	_, stPath := writeSSHPolicyFixture(t, true, state.TailscaleState{})
	client := &fakeSSHPolicyClient{t: t, stPath: stPath, ensureChange: mesh.PolicyChange{TagOwners: []string{sshPolicyNSTag, mesh.AutomationSourceTag}, SSHRule: true, AutomationSSHRule: true}}

	row, built, err := runSSHPolicyCommand(t, false, client)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(built, []string{sshPolicyToken + "@" + config.TokenDefaultTailnet}) {
		t.Fatalf("client built with %v", built)
	}
	if row.Status != "applied" || row.DryRun || !row.CheckRuleAdded || !row.AutomationSSHRuleAdded || row.AutomationSSHRuleRemoved || len(row.AutomationSSHRuleKeptFor) != 0 {
		t.Fatalf("row = %+v", row)
	}
	if strings.Join(client.calls, ",") != "ensure,validate" || !slices.Equal(client.ensureSources, []string{mesh.AutomationSourceTag}) || !slices.Equal(client.lockHeld, []bool{true}) {
		t.Fatalf("calls=%v sources=%v lockHeld=%v", client.calls, client.ensureSources, client.lockHeld)
	}
	assertAutomationSourceRecorded(t, stPath)
}

// assertAutomationSourceRecorded checks the state that reconcile and the
// revoke check read: the source and tags are recorded, and the automation
// tag owner is not, so reconcile can prune it later.
func assertAutomationSourceRecorded(t *testing.T, stPath string) {
	t.Helper()
	got := loadSSHPolicyStateAt(t, stPath)
	if got.PolicySSHAutomationSource != mesh.AutomationSourceTag || !got.PolicySSHRule || !slices.Equal(got.PolicySSHTags, []string{sshPolicyNSTag}) || !slices.Equal(got.PolicyTagOwners, []string{sshPolicyNSTag}) {
		t.Fatalf("state = %+v", got)
	}
}

// With automation off and no other server recording the shared accept rule,
// ssh-policy revokes it, after state already stopped recording the source.
func TestServerSSHPolicyRevokesAutomationRuleWithoutSharers(t *testing.T) {
	_, stPath := writeSSHPolicyFixture(t, false, state.TailscaleState{PolicySSHRule: true, PolicySSHTags: []string{sshPolicyNSTag}, PolicySSHAutomationSource: mesh.AutomationSourceTag})
	// These servers do not share the rule: another admin user, automation
	// off, or another tailnet.
	otherAdmin := sshPolicyServerConfig("demo", "db", true)
	otherAdmin.Admin.Username = "ops"
	registerSSHPolicyServer(t, otherAdmin, state.TailscaleState{Tailnet: sshPolicyTailnet, PolicySSHAutomationSource: mesh.AutomationSourceTag})
	registerSSHPolicyServer(t, sshPolicyServerConfig("demo", "old", false), state.TailscaleState{Tailnet: sshPolicyTailnet})
	registerSSHPolicyServer(t, sshPolicyServerConfig("demo", "far", true), state.TailscaleState{Tailnet: "other.ts.net", PolicySSHAutomationSource: mesh.AutomationSourceTag})
	client := &fakeSSHPolicyClient{t: t, stPath: stPath, removeChange: mesh.PolicyChange{AutomationSSHRuleRemoved: true}}

	row, _, err := runSSHPolicyCommand(t, false, client)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "applied" || row.AutomationSource != "" || !row.AutomationSSHRuleRemoved || len(row.AutomationSSHRuleKeptFor) != 0 {
		t.Fatalf("row = %+v", row)
	}
	if strings.Join(client.calls, ",") != "ensure,remove,validate" || !slices.Equal(client.ensureSources, []string{""}) || !slices.Equal(client.sourceAtRemove, []string{""}) {
		t.Fatalf("calls=%v sources=%v sourceAtRemove=%v", client.calls, client.ensureSources, client.sourceAtRemove)
	}
	if got := loadSSHPolicyStateAt(t, stPath); got.PolicySSHAutomationSource != "" || !got.PolicySSHRule {
		t.Fatalf("state = %+v", got)
	}
}

// A server with the same tags and admin user that still records the source
// uses the same accept rule; revoking it would cut that server's unattended
// runs off, so the rule stays and the row names who keeps it.
func TestServerSSHPolicyKeepsAutomationRuleForSharers(t *testing.T) {
	_, stPath := writeSSHPolicyFixture(t, false, state.TailscaleState{PolicySSHAutomationSource: mesh.AutomationSourceTag})
	registerSSHPolicyServer(t, sshPolicyServerConfig("demo", "api", true), state.TailscaleState{Tailnet: sshPolicyTailnet, PolicySSHAutomationSource: mesh.AutomationSourceTag})
	registerSSHPolicyServer(t, sshPolicyServerConfig("demo", "app", true), state.TailscaleState{PolicySSHAutomationSource: mesh.AutomationSourceTag})
	client := &fakeSSHPolicyClient{t: t, stPath: stPath}

	row, _, err := runSSHPolicyCommand(t, false, client)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(row.AutomationSSHRuleKeptFor, []string{"demo/api", "demo/app"}) || row.AutomationSSHRuleRemoved {
		t.Fatalf("row = %+v", row)
	}
	if strings.Join(client.calls, ",") != "ensure,validate" {
		t.Fatalf("calls = %v", client.calls)
	}
	if got := loadSSHPolicyStateAt(t, stPath); got.PolicySSHAutomationSource != "" {
		t.Fatalf("own record must follow config even when the rule is kept: %+v", got)
	}
}

// When the sharer scan cannot read a registered server, it cannot prove the
// rule is unused, so it must keep the rule and fail; this server's own
// record from the write before is still saved.
func TestServerSSHPolicyFailsClosedOnUnreadableSharer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T)
		wantErr string
	}{
		{
			name: "missing config",
			setup: func(t *testing.T) {
				registerSSHPolicyEntry(t, state.State{Namespace: "demo", Server: "api", Tailscale: state.TailscaleState{PolicySSHAutomationSource: mesh.AutomationSourceTag}}, filepath.Join(t.TempDir(), "missing.yaml"))
			},
			wantErr: "load registered server demo/api config",
		},
		{
			name: "invalid config",
			setup: func(t *testing.T) {
				bad := sshPolicyServerConfig("demo", "api", true)
				bad.Access.Tailscale.Tags = append(bad.Access.Tailscale.Tags, mesh.AutomationSourceTag)
				path := filepath.Join(t.TempDir(), "api.yaml")
				if err := config.Save(path, bad); err != nil {
					t.Fatal(err)
				}
				registerSSHPolicyEntry(t, state.State{Namespace: "demo", Server: "api", Tailscale: state.TailscaleState{PolicySSHAutomationSource: mesh.AutomationSourceTag}}, path)
			},
			wantErr: "must not include " + mesh.AutomationSourceTag,
		},
		{
			name: "no config path",
			setup: func(t *testing.T) {
				registerSSHPolicyEntry(t, state.State{Namespace: "demo", Server: "api"}, "")
			},
			wantErr: "registered server demo/api has no state or config path",
		},
		{
			name: "unreadable state",
			setup: func(t *testing.T) {
				registerSSHPolicyEntry(t, state.State{Namespace: "demo", Server: "api"}, config.ServerConfigPath("demo", "api"))
				if err := os.WriteFile(config.ServerStatePath("demo", "api"), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "load registered server demo/api state",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stPath := writeSSHPolicyFixture(t, false, state.TailscaleState{PolicySSHAutomationSource: mesh.AutomationSourceTag})
			tc.setup(t)
			client := &fakeSSHPolicyClient{t: t, stPath: stPath}

			_, _, err := runSSHPolicyCommand(t, false, client)
			if err == nil || !strings.Contains(err.Error(), "decide whether to revoke the automation SSH rule for demo/web") || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if slices.Contains(client.calls, "remove") {
				t.Fatalf("revoked without proof the rule is unused: %v", client.calls)
			}
			if got := loadSSHPolicyStateAt(t, stPath); got.PolicySSHAutomationSource != "" {
				t.Fatalf("own record not saved after the write: %+v", got)
			}
		})
	}
}

// State is recorded before the read-back, so a failed validation never leaves
// a live accept rule that state does not know about; a failed write records
// nothing.
func TestServerSSHPolicyRecordsStateBeforeValidation(t *testing.T) {
	_, stPath := writeSSHPolicyFixture(t, true, state.TailscaleState{})
	validateErr := errors.New("unsafe Tailscale SSH policy: root access must use check mode")
	client := &fakeSSHPolicyClient{t: t, stPath: stPath, ensureChange: mesh.PolicyChange{AutomationSSHRule: true}, validateErr: validateErr}

	if _, _, err := runSSHPolicyCommand(t, false, client); !errors.Is(err, validateErr) {
		t.Fatalf("err = %v", err)
	}
	if got := loadSSHPolicyStateAt(t, stPath); got.PolicySSHAutomationSource != mesh.AutomationSourceTag || !slices.Equal(got.PolicySSHTags, []string{sshPolicyNSTag}) {
		t.Fatalf("state = %+v", got)
	}

	_, stPath = writeSSHPolicyFixture(t, true, state.TailscaleState{})
	writeErr := errors.New("412 precondition failed")
	client = &fakeSSHPolicyClient{t: t, stPath: stPath, ensureErr: writeErr}
	if _, _, err := runSSHPolicyCommand(t, false, client); !errors.Is(err, writeErr) || slices.Contains(client.calls, "validate") {
		t.Fatalf("err=%v calls=%v", err, client.calls)
	}
	if got := loadSSHPolicyStateAt(t, stPath); got.PolicySSHAutomationSource != "" {
		t.Fatalf("failed write recorded: %+v", got)
	}
}

// Without a stored Tailscale token a non-interactive run must stop before a
// client is even built, instead of sending an unauthenticated policy write.
func TestServerSSHPolicyRequiresTailscaleToken(t *testing.T) {
	cfg, stPath := writeSSHPolicyFixture(t, true, state.TailscaleState{})
	if err := os.Remove(config.Expand(cfg.Credentials.JSONPath)); err != nil {
		t.Fatal(err)
	}
	client := &fakeSSHPolicyClient{t: t, stPath: stPath}

	_, built, err := runSSHPolicyCommand(t, false, client)
	if err == nil || !strings.Contains(err.Error(), "Tailscale API token") || len(built) != 0 {
		t.Fatalf("err=%v built=%v", err, built)
	}
}

// doctor --fix goes through the same locked, recorded write, but must never
// revoke the shared accept rule: that needs the registry check in ssh-policy.
// It also ignores the arguments doctor passes and uses its own config.
func TestDoctorSSHPolicyFixerUsesLockedWriteWithoutRevoke(t *testing.T) {
	cfg, stPath := writeSSHPolicyFixture(t, false, state.TailscaleState{PolicySSHAutomationSource: mesh.AutomationSourceTag})
	client := &fakeSSHPolicyClient{t: t, stPath: stPath, ensureChange: mesh.PolicyChange{SSHRule: true}}
	fixer := doctorSSHPolicyFixer{cfg: cfg, stPath: stPath, policy: client}

	change, err := fixer.EnsureServerproPolicy(context.Background(), []string{"tag:ignored"}, "ignored", "ignored", mesh.AutomationSourceTag)
	if err != nil {
		t.Fatal(err)
	}
	if !change.SSHRule || change.AutomationSSHRuleRemoved {
		t.Fatalf("change = %+v", change)
	}
	if strings.Join(client.calls, ",") != "ensure,validate" || !slices.Equal(client.ensureSources, []string{""}) || !slices.Equal(client.lockHeld, []bool{true}) {
		t.Fatalf("calls=%v sources=%v lockHeld=%v", client.calls, client.ensureSources, client.lockHeld)
	}
	if got := loadSSHPolicyStateAt(t, stPath); got.PolicySSHAutomationSource != "" || !got.PolicySSHRule {
		t.Fatalf("state = %+v", got)
	}
}

// Without a state path the repair could not record what it wrote, so it
// must refuse before any policy call instead of an unrecorded write.
func TestDoctorSSHPolicyFixerRefusesWithoutStatePath(t *testing.T) {
	cfg, _ := writeSSHPolicyFixture(t, true, state.TailscaleState{})
	client := &fakeSSHPolicyClient{t: t}
	fixer := doctorSSHPolicyFixer{cfg: cfg, policy: client}

	if _, err := fixer.EnsureServerproPolicy(context.Background(), nil, "", "", ""); !errors.Is(err, errDoctorFixNoStatePath) {
		t.Fatalf("err = %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("calls = %v", client.calls)
	}
}

// Unattended sessions must go through the tagged node only when config opts
// in; otherwise the operator's own device (and its check rule) is used. The
// node also gets the server tags, so it can refuse a peer outside them.
func TestRemoteRunnerUsesAutomationNodeOnlyWhenEnabled(t *testing.T) {
	on := sshPolicyServerConfig("demo", "web", true)
	runner := remoteRunner(on, "sudo-secret")
	if runner.SudoPassword != "sudo-secret" || runner.Automation == nil || runner.Automation.CLI != sshPolicyCLI || runner.Automation.Socket != sshPolicySocket || !slices.Equal(runner.Automation.Tags, []string{sshPolicyNSTag}) {
		t.Fatalf("runner = %+v", runner)
	}

	off := sshPolicyServerConfig("demo", "web", false)
	off.Access.Tailscale.Automation = config.TailscaleAutomation{Socket: sshPolicySocket, CLI: sshPolicyCLI}
	if runner := remoteRunner(off, "sudo-secret"); runner.Automation != nil || runner.SudoPassword != "sudo-secret" {
		t.Fatalf("runner = %+v", runner)
	}
}

// Without a hook the client must be the real Tailscale adapter, so ssh-policy
// shares the provision write and read-back code; the hook replaces it fully.
func TestNewServerSSHPolicyClientUsesAdapterOrHook(t *testing.T) {
	if _, ok := (&app{}).newServerSSHPolicyClient(sshPolicyToken, "-").(tailscale.Client); !ok {
		t.Fatal("ssh-policy client is not the Tailscale adapter")
	}
	fake := &fakeSSHPolicyClient{t: t}
	var built []string
	if got := sshPolicyApp(io.Discard, false, fake, &built).newServerSSHPolicyClient(sshPolicyToken, sshPolicyTailnet); got != fake || !slices.Equal(built, []string{sshPolicyToken + "@" + sshPolicyTailnet}) {
		t.Fatalf("hook not used: got=%T built=%v", got, built)
	}
}
