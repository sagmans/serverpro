package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestRemoteChecksGitIdentityGatedOnAccountKeyAccess(t *testing.T) {
	cfg := config.Example("prod")
	results := remoteChecksWithOptions(context.Background(), cfg, &fakeRemote{}, "prod-01", Options{})
	for _, name := range []string{"git identity", "github ssh auth", "gh auth", "git signing"} {
		if hasResult(Report{Results: results}, name, Pass, "") {
			t.Fatalf("%q should be skipped without git.access account-key", name)
		}
	}

	cfg.Git.Access = "account-key"
	cfg.Git.Identity = config.GitIdentity{Name: "buzz", Email: "buzz@example.com"}
	cfg.Git.Signing = true
	results = remoteChecksWithOptions(context.Background(), cfg, &fakeRemote{}, "prod-01", Options{})
	for _, name := range []string{"git identity", "github ssh auth", "gh auth", "git signing"} {
		if !hasResult(Report{Results: results}, name, Pass, "") {
			t.Fatalf("missing %q result with account-key access: %+v", name, results)
		}
	}
}

func TestGitIdentityCommandsRunAsAdminUser(t *testing.T) {
	identity := config.GitIdentity{Name: "buzz o'hara", Email: "buzz@example.com"}
	read := gitIdentityReadCommand("deploy")
	fix := gitIdentityFixCommand("deploy", identity)
	for _, want := range []string{"runuser -u 'deploy'", "git config --global user.name", "git config --global user.email"} {
		if !strings.Contains(read, want) {
			t.Fatalf("read command missing %q: %s", want, read)
		}
	}
	for _, want := range []string{"user.name 'buzz o'\\''hara'", "user.email 'buzz@example.com'"} {
		if !strings.Contains(fix, want) {
			t.Fatalf("fix command missing %q: %s", want, fix)
		}
	}
	auth := githubSSHAuthReadCommand("deploy")
	for _, want := range []string{"BatchMode=yes", "successfully authenticated"} {
		if !strings.Contains(auth, want) {
			t.Fatalf("ssh auth command missing %q: %s", want, auth)
		}
	}
	gh := ghAuthReadCommand("deploy")
	if !strings.Contains(gh, "mise\\\" exec -- gh auth status") && !strings.Contains(gh, "exec -- gh auth status") {
		t.Fatalf("gh auth command unexpected: %s", gh)
	}
}
