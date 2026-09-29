package cli

import (
	"io"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
)

// TestMergeCredentialsKeepsConcurrentCredential is the regression guard for the
// stale snapshot write: a run that loaded credentials before another run rotated
// one used to publish its whole snapshot and roll that rotation back.
func TestMergeCredentialsKeepsConcurrentCredential(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	cfg.Cloudflare.AccountID = "acc"
	if err := credentials.SavePartial(cfg, credentials.Set{Namespace: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts-rotated"}); err != nil {
		t.Fatal(err)
	}
	a := &app{stdout: io.Discard}
	staleSnapshot := credentials.Set{Namespace: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts-stale", Cloudflare: "cf"}
	if err := a.mergeCredentials(cfg, staleSnapshot); err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.LoadPartial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Tailscale != "ts-rotated" {
		t.Fatalf("concurrent rotation was overwritten: %+v", creds)
	}
	if creds.Cloudflare != "cf" {
		t.Fatalf("supplied value was not stored: %+v", creds)
	}
}

func TestStoreSudoPasswordKeepsConcurrentCredential(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	if err := credentials.SavePartial(cfg, credentials.Set{Namespace: "demo", Server: "web", ServerProvider: "acct", GitHubPAT: "ghp_concurrent"}); err != nil {
		t.Fatal(err)
	}
	a := &app{stdout: io.Discard}
	if err := a.storeSudoPassword(cfg, "stored-sudo-password-1234"); err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.LoadPartial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.GitHubPAT != "ghp_concurrent" || creds.AdminSudoPassword != "stored-sudo-password-1234" {
		t.Fatalf("stored credentials = %+v", creds)
	}
}
