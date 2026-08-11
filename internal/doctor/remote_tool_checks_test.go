package doctor

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/tailscaletools"
)

func TestRemoteToolChecksPassWhenInstalled(t *testing.T) {
	cfg := config.Example("prod")
	checks := bootstraptools.Checks(cfg.Admin.Username)
	toolEvidence := []struct {
		name string
		out  string
	}{
		{name: "git", out: "git version 2.43.0\nOpenSSH_9.6p1"},
		{name: "docker engine", out: "Docker version 29.0.0\nactive"},
		{name: "docker compose", out: "Docker Compose version v5.1.2"},
		{name: "htop", out: "htop 3.4.1"},
		{name: "managed package updates", out: "current"},
		{name: "mise", out: bootstraptools.MinimumMiseVersion},
		{name: "node " + bootstraptools.NodeVersion, out: "v" + bootstraptools.NodeVersion},
		{name: "npm", out: "11.0.0"},
		{name: "pi " + bootstraptools.PiVersion, out: bootstraptools.PiVersion},
		{name: "uv " + bootstraptools.UVVersion, out: "uv " + bootstraptools.UVVersion},
		{name: "rust " + bootstraptools.RustVersion, out: "rustc " + bootstraptools.RustVersion + "\ncargo " + bootstraptools.RustVersion + "\nrustfmt " + bootstraptools.RustVersion + "\nclippy 0.1.97\nrust-docs-x86_64-unknown-linux-gnu"},
		{name: "tmux " + bootstraptools.TmuxVersion, out: "tmux " + bootstraptools.TmuxVersion},
		{name: "gh " + bootstraptools.GitHubCLIVersion, out: "gh version " + bootstraptools.GitHubCLIVersion},
		{name: "rg " + bootstraptools.RipgrepVersion, out: "ripgrep " + bootstraptools.RipgrepVersion},
		{name: "fd " + bootstraptools.FdVersion, out: "fd " + bootstraptools.FdVersion},
		{name: "ast-grep " + bootstraptools.AstGrepVersion, out: "ast-grep " + bootstraptools.AstGrepVersion},
		{name: "sem " + bootstraptools.SemVersion, out: "sem " + bootstraptools.SemVersion},
		{name: "inspect " + bootstraptools.InspectVersion, out: "inspect " + bootstraptools.InspectVersion + "\nsha256 " + bootstraptools.InspectLinuxX64SHA256},
		{name: "herdr " + bootstraptools.HerdrVersion, out: "herdr " + bootstraptools.HerdrVersion + "\nsha256 " + bootstraptools.HerdrLinuxX64SHA256},
		{name: "herdr pi integration", out: "pi: current"},
	}
	responses := make(map[string][]remoteCall, len(toolEvidence))
	for _, tool := range toolEvidence {
		responses[remoteToolCheckByName(t, checks, tool.name).Command] = []remoteCall{{out: tool.out}}
	}
	responses[tailscaletools.CheckCommand()] = []remoteCall{{out: "client=" + tailscaletools.Version + " daemon=" + tailscaletools.Version}}
	r := &scriptedRemote{responses: responses}
	results := remoteToolChecks(context.Background(), r, cfg.Admin.Username, "prod-01", Options{})
	for _, tool := range toolEvidence {
		if !hasResult(Report{Results: results}, tool.name, Pass, "") {
			t.Fatalf("missing pass result for %s: %+v", tool.name, results)
		}
	}
	if !hasResult(Report{Results: results}, tailscaletools.CheckName, Pass, tailscaletools.Version) {
		t.Fatalf("missing Tailscale pass result: %+v", results)
	}
	if hasCommand(r.commands, "serverpro-bootstrap-tools") {
		t.Fatalf("bootstrap should not run when tools are installed: %#v", r.commands)
	}
}

func TestRemoteToolChecksFixRunsBootstrapOnce(t *testing.T) {
	cfg := config.Example("prod")
	checks := bootstraptools.Checks(cfg.Admin.Username)
	missingTools := []struct {
		name string
		err  string
		out  string
	}{
		{name: "node " + bootstraptools.NodeVersion, err: "node missing", out: "v" + bootstraptools.NodeVersion},
		{name: "pi " + bootstraptools.PiVersion, err: "pi missing", out: bootstraptools.PiVersion},
		{name: "uv " + bootstraptools.UVVersion, err: "uv missing", out: "uv " + bootstraptools.UVVersion},
		{name: "rust " + bootstraptools.RustVersion, err: "rust missing", out: "rustc " + bootstraptools.RustVersion + "\ncargo " + bootstraptools.RustVersion + "\nrustfmt " + bootstraptools.RustVersion + "\nclippy 0.1.97\nrust-docs-x86_64-unknown-linux-gnu"},
		{name: "rg " + bootstraptools.RipgrepVersion, err: "rg missing", out: "ripgrep " + bootstraptools.RipgrepVersion},
		{name: "ast-grep " + bootstraptools.AstGrepVersion, err: "ast-grep missing", out: "ast-grep " + bootstraptools.AstGrepVersion},
		{name: "sem " + bootstraptools.SemVersion, err: "sem missing", out: "sem " + bootstraptools.SemVersion},
		{name: "inspect " + bootstraptools.InspectVersion, err: "inspect missing", out: "inspect " + bootstraptools.InspectVersion},
		{name: "herdr " + bootstraptools.HerdrVersion, err: "herdr missing", out: "herdr " + bootstraptools.HerdrVersion},
		{name: "herdr pi integration", err: "pi integration missing", out: "pi: current"},
		{name: "managed package updates", err: "updates available", out: "current"},
	}
	responses := make(map[string][]remoteCall, len(missingTools))
	for _, tool := range missingTools {
		responses[remoteToolCheckByName(t, checks, tool.name).Command] = []remoteCall{{err: errors.New(tool.err)}, {out: tool.out}}
	}
	r := &scriptedRemote{responses: responses}
	results := remoteToolChecks(context.Background(), r, cfg.Admin.Username, "prod-01", Options{Fix: true})
	for _, tool := range missingTools {
		if !hasResult(Report{Results: results}, tool.name, Pass, "fixed") {
			t.Fatalf("missing fixed result for %s: %+v", tool.name, results)
		}
	}
	count := 0
	for _, command := range r.commands {
		if strings.Contains(command, "serverpro-bootstrap-tools") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("bootstrap should run once, ran %d times: %#v", count, r.commands)
	}
}

func TestRemoteToolChecksFixRefreshesManagedPackagesWhenCachedChecksPass(t *testing.T) {
	cfg := config.Example("prod")
	packageCheck := remoteToolCheckByName(t, bootstraptools.Checks(cfg.Admin.Username), bootstraptools.ManagedPackageCheckName)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		packageCheck.Command: {
			{out: "current"},
			{err: errors.New("updates discovered after refresh")},
			{out: "current"},
		},
	}}
	results := remoteToolChecks(context.Background(), r, cfg.Admin.Username, "prod-01", Options{Fix: true})
	if !hasResult(Report{Results: results}, bootstraptools.ManagedPackageCheckName, Pass, "fixed") {
		t.Fatalf("missing proactive package refresh result: %+v", results)
	}
	if !slices.Contains(r.commands, bootstraptools.ManagedPackageRefreshCommand()) {
		t.Fatalf("explicit --fix did not refresh package metadata: %#v", r.commands)
	}
	count := 0
	for _, command := range r.commands {
		if strings.Contains(command, "serverpro-bootstrap-tools") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("newly discovered updates should run bootstrap once, ran %d times: %#v", count, r.commands)
	}
}

func TestRemoteToolChecksFixUpdatesTailscaleSeparately(t *testing.T) {
	cfg := config.Example("prod")
	responses := map[string][]remoteCall{
		tailscaletools.CheckCommand(): {
			{err: errors.New("Tailscale stale")},
			{out: "client=" + tailscaletools.Version + " daemon=" + tailscaletools.Version},
		},
		tailscaletools.UpdateScript(): {{out: "restart scheduled"}},
	}
	r := &scriptedRemote{responses: responses}
	waits := 0
	results := remoteToolChecksWithWait(context.Background(), r, cfg.Admin.Username, "prod-01", Options{Fix: true}, func(context.Context) error {
		waits++
		return nil
	})
	if !hasResult(Report{Results: results}, tailscaletools.CheckName, Pass, "fixed") {
		t.Fatalf("missing fixed Tailscale result: %+v", results)
	}
	if waits != 1 {
		t.Fatalf("restart waits = %d, want 1", waits)
	}
	if hasCommand(r.commands, "serverpro-bootstrap-tools") {
		t.Fatalf("generic bootstrap ran for Tailscale-only failure: %#v", r.commands)
	}
	if !slices.Contains(r.commands, tailscaletools.UpdateScript()) {
		t.Fatalf("Tailscale updater missing: %#v", r.commands)
	}
}

func TestRemoteToolChecksTailscaleRepairFailurePaths(t *testing.T) {
	cfg := config.Example("prod")
	cases := []struct {
		name         string
		update       remoteCall
		waitErr      error
		wantEvidence string
		wantWaits    int
	}{
		{
			name:         "updater-fails",
			update:       remoteCall{err: errors.New("Tailscale update failed")},
			wantEvidence: "fix failed: Tailscale update failed",
		},
		{
			name:         "restart-wait-fails",
			update:       remoteCall{out: "restart scheduled"},
			waitErr:      errors.New("restart wait canceled"),
			wantEvidence: "fix failed: restart wait canceled",
			wantWaits:    1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &scriptedRemote{responses: map[string][]remoteCall{
				tailscaletools.CheckCommand(): {{err: errors.New("Tailscale stale")}},
				tailscaletools.UpdateScript(): {tc.update},
			}}
			waits := 0
			results := remoteToolChecksWithWait(context.Background(), r, cfg.Admin.Username, "prod-01", Options{Fix: true}, func(context.Context) error {
				waits++
				return tc.waitErr
			})
			if !hasResult(Report{Results: results}, tailscaletools.CheckName, Fail, tc.wantEvidence) {
				t.Fatalf("missing Tailscale repair failure %q: %+v", tc.wantEvidence, results)
			}
			if waits != tc.wantWaits {
				t.Fatalf("waits = %d, want %d", waits, tc.wantWaits)
			}
			if hasCommand(r.commands, "serverpro-bootstrap-tools") {
				t.Fatalf("generic bootstrap ran for Tailscale repair failure: %#v", r.commands)
			}
		})
	}
}

func TestRemoteToolChecksTailscaleRepairRetriesReconnection(t *testing.T) {
	cfg := config.Example("prod")
	r := &scriptedRemote{responses: map[string][]remoteCall{
		tailscaletools.CheckCommand(): {
			{err: errors.New("Tailscale stale")},
			{err: errors.New("tailscaled restarting")},
			{out: "client=" + tailscaletools.Version + " daemon=" + tailscaletools.Version},
		},
		tailscaletools.UpdateScript(): {{out: "restart scheduled"}},
	}}
	waits := 0
	results := remoteToolChecksWithWait(context.Background(), r, cfg.Admin.Username, "prod-01", Options{Fix: true}, func(context.Context) error {
		waits++
		return nil
	})
	if !hasResult(Report{Results: results}, tailscaletools.CheckName, Pass, "fixed") {
		t.Fatalf("missing fixed Tailscale result after retry: %+v", results)
	}
	if waits != 2 {
		t.Fatalf("waits = %d, want restart grace plus one retry", waits)
	}
}

func TestRemoteToolChecksKeepWarningOutputRaw(t *testing.T) {
	cfg := config.Example("prod")
	check := remoteToolCheckByName(t, bootstraptools.Checks(cfg.Admin.Username), "mise")
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check.Command: {{out: "2026.7.12\n[WARN] config: error parsing config file: <admin-home>/.config/mise/config.toml"}},
	}}
	results := remoteToolChecks(context.Background(), r, cfg.Admin.Username, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "mise", Pass, "error parsing config file") {
		t.Fatalf("missing raw warning evidence: %+v", results)
	}
	if hasCommand(r.commands, "serverpro-bootstrap-tools") {
		t.Fatalf("bootstrap should not run without failed checks: %#v", r.commands)
	}
}

func TestRemoteToolChecksWithoutFixFailsMissingTool(t *testing.T) {
	cfg := config.Example("prod")
	piName := "pi " + bootstraptools.PiVersion
	check := remoteToolCheckByName(t, bootstraptools.Checks(cfg.Admin.Username), piName)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check.Command: {{err: errors.New("pi missing")}},
	}}
	results := remoteToolChecks(context.Background(), r, cfg.Admin.Username, "prod-01", Options{})
	if !hasResult(Report{Results: results}, piName, Fail, "pi missing") {
		t.Fatalf("missing tool failure: %+v", results)
	}
	// Pin the remediation hint: it must name the real command
	// (`serverpro server doctor`), not a non-existent `server doctor`.
	for _, res := range results {
		if res.Name == piName && res.Remediation != "run serverpro server doctor --fix" {
			t.Fatalf("pi remediation = %q, want serverpro server doctor --fix", res.Remediation)
		}
	}
	if hasCommand(r.commands, "serverpro-bootstrap-tools") {
		t.Fatalf("bootstrap should not run without --fix: %#v", r.commands)
	}
}

// TestRemoteToolChecksFixFailedBootstrap covers the path where the bootstrap
// install script itself errors: every originally-failed check must be annotated
// with "; fix failed: <err>" and remediation "inspect remote command".
func TestRemoteToolChecksFixFailedBootstrap(t *testing.T) {
	cfg := config.Example("prod")
	user := cfg.Admin.Username
	checks := bootstraptools.Checks(user)
	nodeName := "node " + bootstraptools.NodeVersion
	nodeCheck := remoteToolCheckByName(t, checks, nodeName)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		nodeCheck.Command:                         {{err: errors.New("node missing")}},
		bootstraptools.InstallScriptForUser(user): {{err: errors.New("bootstrap install failed: exit 42")}},
	}}
	results := remoteToolChecks(context.Background(), r, user, "prod-01", Options{Fix: true})
	var node *Result
	for i := range results {
		if results[i].Name == nodeName {
			node = &results[i]
			break
		}
	}
	if node == nil || node.Status != Fail {
		t.Fatalf("node should fail when bootstrap fix fails: %+v", results)
	}
	if !strings.Contains(node.Evidence, "; fix failed: bootstrap install failed") {
		t.Fatalf("node evidence = %q, want '; fix failed:' annotation", node.Evidence)
	}
	if node.Remediation != "inspect remote command" {
		t.Fatalf("node remediation = %q, want inspect remote command", node.Remediation)
	}
}

// TestRemoteToolChecksFixAppliedButStillFails covers a successful bootstrap fix
// followed by a check that still fails on retry: remediation must read
// "fix applied but check still failed" rather than a misleading success.
func TestRemoteToolChecksFixAppliedButStillFails(t *testing.T) {
	cfg := config.Example("prod")
	user := cfg.Admin.Username
	checks := bootstraptools.Checks(user)
	piName := "pi " + bootstraptools.PiVersion
	piCheck := remoteToolCheckByName(t, checks, piName)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		// Fails initially, then fails again after the (default-success) fix.
		piCheck.Command: {{err: errors.New("pi missing")}, {err: errors.New("pi still missing")}},
	}}
	results := remoteToolChecks(context.Background(), r, user, "prod-01", Options{Fix: true})
	var pi *Result
	for i := range results {
		if results[i].Name == piName {
			pi = &results[i]
			break
		}
	}
	if pi == nil || pi.Status != Fail {
		t.Fatalf("pi should fail after a fix that did not help: %+v", results)
	}
	if pi.Remediation != "fix applied but check still failed" {
		t.Fatalf("pi remediation = %q, want fix applied but check still failed", pi.Remediation)
	}
}
