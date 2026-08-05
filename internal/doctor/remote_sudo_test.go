package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

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

func TestRemoteChecksWithFixRejectsMalformedPasswordHash(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("admin sudo permits NOPASSWD:ALL")}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true, SudoPassword: "correct horse battery staple", SudoPasswordHash: "not-a-valid-hash"})
	if !hasResult(Report{Results: results}, "sudo password required", Fail, "invalid sudo password hash") {
		t.Fatalf("missing malformed hash failure: %+v", results)
	}
	if hasCommand(r.commands, "chpasswd --encrypted") {
		t.Fatalf("malformed hash should not run fix: %#v", r.commands)
	}
}

func TestRemoteChecksWithFixRejectsMissingSudoPassword(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	hash := "$6$rounds=100000$abcdefghijklmnop$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("admin sudo permits NOPASSWD:ALL")}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true, SudoPasswordHash: hash})
	if !hasResult(Report{Results: results}, "sudo password required", Fail, "invalid sudo password") {
		t.Fatalf("missing invalid password failure: %+v", results)
	}
	if hasCommand(r.commands, "chpasswd --encrypted") {
		t.Fatalf("missing sudo password should not run fix: %#v", r.commands)
	}
}

func TestRemoteChecksWithFixRequiresPasswordForSudo(t *testing.T) {
	cfg := config.Example("prod")
	check := sudoPasswordRequiredCommand(cfg.Admin.Username)
	fix := sudoPasswordFixCommand(cfg.Admin.Username)
	hash := "$6$rounds=100000$abcdefghijklmnop$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	r := &scriptedRemote{responses: map[string][]remoteCall{
		check: {{err: errors.New("admin sudo permits NOPASSWD:ALL")}, {out: "admin sudo requires password"}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{Fix: true, SudoPassword: "correct horse battery staple", SudoPasswordHash: hash})
	if !hasResult(Report{Results: results}, "sudo password required", Pass, "fixed") {
		t.Fatalf("missing fixed sudo result: %+v", results)
	}
	if !hasCommand(r.commands, "chpasswd --encrypted") || !hasCommand(r.commands, "visudo -cf") || !hasCommand(r.commands, "/var/backups/serverpro") || !hasCommand(r.commands, "fail_after_backup") || !hasCommand(r.commands, "visudo -c") {
		t.Fatalf("fix command missing safety steps: %#v", r.commands)
	}
	if hasCommand(r.commands, hash) || hasCommand(r.commands, "correct horse battery staple") {
		t.Fatalf("fix script leaked secret in command: %#v", r.commands)
	}
	if len(r.inputs) != 1 || r.inputs[0] != sudoPasswordFixInput(hash, "correct horse battery staple") {
		t.Fatalf("fix input = %#v", r.inputs)
	}
	if !hasCommand([]string{fix}, "sudo -S -p '' -v") || !hasCommand([]string{fix}, "fail_after_backup") {
		t.Fatalf("fix command should validate sudo password with rollback: %s", fix)
	}
	if !hasCommand([]string{fix}, "runuser -u") {
		t.Fatalf("fix command should validate admin sudo state: %s", fix)
	}
}
