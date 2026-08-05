package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/config"
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
		{name: "mise", out: "2026.7.12"},
		{name: "node " + bootstraptools.NodeVersion, out: "v" + bootstraptools.NodeVersion},
		{name: "npm", out: "11.0.0"},
		{name: "pi " + bootstraptools.PiVersion, out: bootstraptools.PiVersion},
		{name: "tmux " + bootstraptools.TmuxVersion, out: "tmux " + bootstraptools.TmuxVersion},
		{name: "gh " + bootstraptools.GitHubCLIVersion, out: "gh version " + bootstraptools.GitHubCLIVersion},
		{name: "rg " + bootstraptools.RipgrepVersion, out: "ripgrep " + bootstraptools.RipgrepVersion},
		{name: "fd " + bootstraptools.FdVersion, out: "fd " + bootstraptools.FdVersion},
		{name: "herdr " + bootstraptools.HerdrVersion, out: "herdr " + bootstraptools.HerdrVersion + "\nsha256 " + bootstraptools.HerdrLinuxX64SHA256},
		{name: "herdr pi integration", out: "pi: current (v6)"},
	}
	responses := make(map[string][]remoteCall, len(toolEvidence))
	for _, tool := range toolEvidence {
		responses[remoteToolCheckByName(t, checks, tool.name).Command] = []remoteCall{{out: tool.out}}
	}
	r := &scriptedRemote{responses: responses}
	results := remoteToolChecks(context.Background(), r, cfg.Admin.Username, "prod-01", Options{})
	for _, tool := range toolEvidence {
		if !hasResult(Report{Results: results}, tool.name, Pass, "") {
			t.Fatalf("missing pass result for %s: %+v", tool.name, results)
		}
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
		{name: "rg " + bootstraptools.RipgrepVersion, err: "rg missing", out: "ripgrep " + bootstraptools.RipgrepVersion},
		{name: "herdr " + bootstraptools.HerdrVersion, err: "herdr missing", out: "herdr " + bootstraptools.HerdrVersion},
		{name: "herdr pi integration", err: "pi integration missing", out: "pi: current (v6)"},
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
