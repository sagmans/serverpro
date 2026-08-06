package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	read := gitIdentityReadCommand("deploy", identity)
	fix := gitIdentityFixCommand("deploy", identity)
	for _, want := range []string{"runuser -u 'deploy'", "git config --global", "user.name", "user.email", "buzz o'\\''hara", "buzz@example.com"} {
		if !strings.Contains(read, want) {
			t.Fatalf("read command missing %q: %s", want, read)
		}
	}
	for _, want := range []string{"user.name 'buzz o'\\''hara'", "user.email 'buzz@example.com'"} {
		if !strings.Contains(fix, want) {
			t.Fatalf("fix command missing %q: %s", want, fix)
		}
	}
	signing := gitSigningReadCommand("deploy")
	for _, want := range []string{"gpg.format", "user.signingkey", "id_ed25519_sign.pub", "commit.gpgsign"} {
		if !strings.Contains(signing, want) {
			t.Fatalf("signing read command missing %q: %s", want, signing)
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

func TestGitIdentityReadAndFixCommandsEnforceExactIntent(t *testing.T) {
	fixture := newDoctorGitCommandFixture(t)
	identity := config.GitIdentity{Name: "buzz", Email: "buzz@example.com"}
	if err := fixture.run(gitIdentityFixCommand("deploy", identity)); err != nil {
		t.Fatalf("identity fix failed: %v", err)
	}
	if err := fixture.run(gitIdentityReadCommand("deploy", identity)); err != nil {
		t.Fatalf("exact identity rejected: %v", err)
	}
	for _, drift := range []struct {
		key   string
		value string
	}{
		{key: "user.name", value: "wrong"},
		{key: "user.email", value: "wrong@example.com"},
	} {
		t.Run(drift.key, func(t *testing.T) {
			fixture.gitConfig(t, drift.key, drift.value)
			if err := fixture.run(gitIdentityReadCommand("deploy", identity)); err == nil {
				t.Fatalf("%s drift passed doctor", drift.key)
			}
			if err := fixture.run(gitIdentityFixCommand("deploy", identity)); err != nil {
				t.Fatalf("identity repair failed: %v", err)
			}
		})
	}
}

func TestGitSigningReadAndFixCommandsEnforceExactIntent(t *testing.T) {
	fixture := newDoctorGitCommandFixture(t)
	if err := fixture.run(gitSigningFixCommand("deploy")); err != nil {
		t.Fatalf("signing fix failed: %v", err)
	}
	if err := fixture.run(gitSigningReadCommand("deploy")); err != nil {
		t.Fatalf("exact signing intent rejected: %v", err)
	}
	for _, drift := range []struct {
		key   string
		value string
	}{
		{key: "gpg.format", value: "openpgp"},
		{key: "user.signingkey", value: "/tmp/wrong.pub"},
		{key: "commit.gpgsign", value: "false"},
	} {
		t.Run(drift.key, func(t *testing.T) {
			fixture.gitConfig(t, drift.key, drift.value)
			if err := fixture.run(gitSigningReadCommand("deploy")); err == nil {
				t.Fatalf("%s drift passed doctor", drift.key)
			}
			if err := fixture.run(gitSigningFixCommand("deploy")); err != nil {
				t.Fatalf("signing repair failed: %v", err)
			}
		})
	}
}

func TestGitIdentityFixCommandPreservesFirstFailure(t *testing.T) {
	fixture := newDoctorGitCommandFixture(t)
	fakeGit := `#!/bin/sh
case "$*" in *user.name*) exit 1 ;; esac
exit 0
`
	fixture.writeExecutable(t, "git", fakeGit)
	identity := config.GitIdentity{Name: "buzz", Email: "buzz@example.com"}
	if err := fixture.run(gitIdentityFixCommand("deploy", identity)); err == nil {
		t.Fatal("failed user.name update was masked by later user.email update")
	}
}

type doctorGitCommandFixture struct {
	binDir string
	home   string
}

func newDoctorGitCommandFixture(t *testing.T) doctorGitCommandFixture {
	t.Helper()
	fixture := doctorGitCommandFixture{binDir: t.TempDir(), home: t.TempDir()}
	fixture.writeExecutable(t, "getent", "#!/bin/sh\nprintf 'deploy:x:1000:1000:Deploy:%s:/bin/sh\\n' \"$DOCTOR_GIT_HOME\"\n")
	fixture.writeExecutable(t, "runuser", "#!/bin/sh\nset -eu\n[ \"$1\" = -u ]\nshift 2\n[ \"$1\" = -- ]\nshift\nexec \"$@\"\n")
	return fixture
}

func (f doctorGitCommandFixture) writeExecutable(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.binDir, name), []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func (f doctorGitCommandFixture) run(script string) error {
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "DOCTOR_GIT_HOME="+f.home, "PATH="+f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cmd.Run()
}

func (f doctorGitCommandFixture) gitConfig(t *testing.T, key, value string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--global", key, value)
	cmd.Env = append(os.Environ(), "HOME="+f.home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config %s: %v: %s", key, err, out)
	}
}
