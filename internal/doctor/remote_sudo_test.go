package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestRemoteChecksCodesSudoAuthenticationFailure(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("permission denied")}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	for _, result := range results {
		if result.Name == "sudo password required" && result.Code == SudoPasswordAuthFailureCode {
			return
		}
	}
	t.Fatalf("sudo auth result code missing: %+v", results)
}

func TestRemoteChecksFailsWhenAdminHasNOPASSWDSudo(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("admin sudo permits NOPASSWD:ALL")}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "sudo password required", Fail, "NOPASSWD") {
		t.Fatalf("missing NOPASSWD sudo failure: %+v", results)
	}
}

func TestRemoteChecksWithFixRejectsMultilineSudoPassword(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("admin sudo permits NOPASSWD:ALL")}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true, SudoPassword: "correct horse\nbattery staple"})
	if !hasResult(Report{Results: results}, "sudo password required", Fail, "invalid sudo password") {
		t.Fatalf("missing invalid password failure: %+v", results)
	}
	if hasCommand(r.commands, "chpasswd") {
		t.Fatalf("multiline password should not run fix: %#v", r.commands)
	}
}

func TestRemoteChecksWithFixRejectsMissingSudoPassword(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("admin sudo permits NOPASSWD:ALL")}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true})
	if !hasResult(Report{Results: results}, "sudo password required", Fail, "invalid sudo password") {
		t.Fatalf("missing invalid password failure: %+v", results)
	}
	if hasCommand(r.commands, "chpasswd") {
		t.Fatalf("missing sudo password should not run fix: %#v", r.commands)
	}
}

func TestRemoteChecksWithFixRequiresPasswordForSudo(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	fix := sudoPasswordFixCommand(cfg.Admin.Username)
	hashCheck := adminPasswordHashCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check:     {{err: errors.New("admin sudo permits NOPASSWD:ALL")}, {out: "admin sudo requires password"}},
		hashCheck: {{err: errors.New("admin password is locked or not set")}, {out: "admin password uses crypt scheme $y$"}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true, SudoPassword: "correct horse battery staple"})
	if !hasResult(Report{Results: results}, "sudo password required", Pass, "fixed") {
		t.Fatalf("missing fixed sudo result: %+v", results)
	}
	if !hasResult(Report{Results: results}, "admin password hash", Pass, "$y$") {
		t.Fatalf("hash check should re-read after the sudo fix instead of fixing twice: %+v", results)
	}
	if !hasCommand(r.commands, "| chpasswd\n") || hasCommand(r.commands, "--encrypted") || !hasCommand(r.commands, "visudo -cf") || !hasCommand(r.commands, "/var/backups/serverpro") || !hasCommand(r.commands, "fail_after_backup") || !hasCommand(r.commands, "visudo -c") {
		t.Fatalf("fix command missing safety steps: %#v", r.commands)
	}
	if hasCommand(r.commands, "correct horse battery staple") {
		t.Fatalf("fix script leaked secret in command: %#v", r.commands)
	}
	if len(r.inputs) != 1 || r.inputs[0] != sudoPasswordFixInput("correct horse battery staple") {
		t.Fatalf("fix input = %#v", r.inputs)
	}
	if !hasCommand([]string{fix}, "sudo -S -p '' -v") || !hasCommand([]string{fix}, "fail_after_backup") {
		t.Fatalf("fix command should validate sudo password with rollback: %s", fix)
	}
	if !hasCommand([]string{fix}, "runuser -u") {
		t.Fatalf("fix command should validate admin sudo state: %s", fix)
	}
}

func TestRemoteChecksFailsOnLegacySHACryptAdminPassword(t *testing.T) {
	cfg := config.Example("prod")
	hashCheck := adminPasswordHashCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		hashCheck: {{out: "admin password uses legacy crypt scheme $6$", err: errors.New("command failed")}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "admin password hash", Fail, "legacy crypt scheme $6$") {
		t.Fatalf("missing legacy hash failure: %+v", results)
	}
	if hasCommand(r.commands, "chpasswd") {
		t.Fatalf("doctor without --fix must not re-set the password: %#v", r.commands)
	}
}

func TestRemoteChecksWithFixResetsLegacySHACryptAdminPassword(t *testing.T) {
	cfg := config.Example("prod")
	hashCheck := adminPasswordHashCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		hashCheck: {{out: "admin password uses legacy crypt scheme $6$", err: errors.New("command failed")}, {out: "admin password uses crypt scheme $y$"}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true, SudoPassword: "correct horse battery staple"})
	if !hasResult(Report{Results: results}, "admin password hash", Pass, "fixed: admin password uses crypt scheme $y$") {
		t.Fatalf("missing fixed hash result: %+v", results)
	}
	if !hasCommand(r.commands, sudoPasswordFixCommand(cfg.Admin.Username)) || len(r.inputs) != 1 || r.inputs[0] != sudoPasswordFixInput("correct horse battery staple") {
		t.Fatalf("fix should re-set the password once over protected stdin: commands=%d inputs=%d", len(r.commands), len(r.inputs))
	}
}

func TestAdminPasswordHashCommandNeverPrintsTheHash(t *testing.T) {
	command := adminPasswordHashCommand("deploy")
	for _, want := range []string{"getent shadow 'deploy'", `'$y$'*`, `'!'*`, "legacy crypt scheme", "cut -d'$' -f2"} {
		if !strings.Contains(command, want) {
			t.Fatalf("hash check missing %q:\n%s", want, command)
		}
	}
	if strings.Contains(command, `"$hash"\n`) || strings.Contains(command, `echo "$hash"`) || strings.Contains(command, `echo "$entry"`) {
		t.Fatalf("hash check must print only the scheme:\n%s", command)
	}
}
