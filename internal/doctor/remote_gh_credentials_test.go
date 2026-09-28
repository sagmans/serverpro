package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
)

const testGHPAT = "ghp_doctor_parity_fixture"

func TestGHTokenParityMatrix(t *testing.T) {
	readCommand := ghCredentialReadCommand("deploy")
	deployScript := lifecycle.GHTokenScript("deploy")
	localFingerprint := credentialFingerprint(testGHPAT)
	dead := "sha=" + localFingerprint + "\nauth=failed"
	cases := []struct {
		name            string
		reads           []remoteCall
		deploy          []remoteCall
		pat             string
		fix             bool
		wantStatus      Status
		wantEvidence    string
		wantRemediation string
		wantDeploy      bool
	}{
		{name: "match", reads: []remoteCall{{out: "sha=" + localFingerprint + "\nauth=ok"}}, pat: testGHPAT, wantStatus: Pass, wantEvidence: "local=remote sha=" + localFingerprint},
		{name: "drift with local copy", reads: []remoteCall{{out: "sha=0123456789ab\nauth=ok"}}, pat: testGHPAT, wantStatus: Warn, wantEvidence: "differs from remote sha=0123456789ab", wantRemediation: ghTokenParityConvergeRemediation},
		{name: "drift without local copy", reads: []remoteCall{{out: "sha=0123456789ab\nauth=ok"}}, wantStatus: Warn, wantEvidence: "no PAT stored locally", wantRemediation: ghTokenParityConvergeRemediation},
		{name: "dead remote without fix", reads: []remoteCall{{out: dead}}, pat: testGHPAT, wantStatus: Warn, wantEvidence: "local sha=" + localFingerprint + " is available", wantRemediation: ghTokenParityDeployHint},
		{name: "dead remote deploys under fix", reads: []remoteCall{{out: dead}, {out: "sha=" + localFingerprint + "\nauth=ok"}}, deploy: []remoteCall{{out: "gh authenticated as buzz"}}, pat: testGHPAT, fix: true, wantStatus: Pass, wantEvidence: "fixed: deployed local sha=" + localFingerprint, wantDeploy: true},
		{name: "rejected deploy recommends rotation", reads: []remoteCall{{out: dead}}, deploy: []remoteCall{{err: errors.New("GitHub PAT validation failed")}}, pat: testGHPAT, fix: true, wantStatus: Fail, wantEvidence: "deploying the stored PAT failed", wantRemediation: ghTokenParityRotateRemediation, wantDeploy: true},
		{name: "recheck still failing recommends rotation", reads: []remoteCall{{out: dead}, {out: "sha=0123456789ab\nauth=failed"}}, deploy: []remoteCall{{out: "gh authenticated as buzz"}}, pat: testGHPAT, fix: true, wantStatus: Fail, wantEvidence: "remote gh auth still fails", wantRemediation: ghTokenParityRotateRemediation, wantDeploy: true},
		{name: "dead remote without local copy", reads: []remoteCall{{out: "sha=0123456789ab\nauth=failed"}}, wantStatus: Warn, wantEvidence: "no PAT is stored locally", wantRemediation: ghTokenParityRotateRemediation},
		{name: "nothing stored anywhere", reads: []remoteCall{{out: "absent\nauth=failed"}}, wantStatus: Skip, wantEvidence: "no github PAT stored"},
		{name: "read failure", reads: []remoteCall{{err: errors.New("ssh: connection lost")}}, pat: testGHPAT, wantStatus: Fail, wantEvidence: "ssh: connection lost", wantRemediation: "inspect remote command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := map[string][]remoteCall{readCommand: tc.reads}
			if tc.deploy != nil {
				responses[deployScript] = tc.deploy
			}
			scripted := &scriptedRemote{responses: responses}
			result := ghTokenParityCheck(context.Background(), scripted, "deploy", "prod-01", readCommand, Options{Fix: tc.fix, GitHubPAT: tc.pat})
			if result.Status != tc.wantStatus {
				t.Fatalf("status = %s, want %s: %+v", result.Status, tc.wantStatus, result)
			}
			if !strings.Contains(result.Evidence, tc.wantEvidence) {
				t.Fatalf("evidence %q missing %q", result.Evidence, tc.wantEvidence)
			}
			if tc.wantRemediation != "" && !strings.Contains(result.Remediation, tc.wantRemediation) {
				t.Fatalf("remediation %q missing %q", result.Remediation, tc.wantRemediation)
			}
			if tc.wantDeploy {
				if len(scripted.inputs) != 1 || scripted.inputs[0] != tc.pat+"\n" {
					t.Fatalf("deploy input = %q, want PAT on stdin only", scripted.inputs)
				}
			} else if len(scripted.inputs) != 0 {
				t.Fatalf("unexpected stdin writes: %q", scripted.inputs)
			}
			if tc.pat != "" && strings.Contains(result.Evidence+result.Remediation, tc.pat) {
				t.Fatal("PAT leaked into evidence or remediation")
			}
		})
	}
}

func TestGHTokenParityPrecedesToolApplyUngated(t *testing.T) {
	cfg := config.Example("prod")
	if cfg.Git.Access == config.GitAccessAccountKey {
		t.Fatal("fixture must not configure account-key access for the ungated assertion")
	}
	plan := buildRemoteReadPlan(cfg)
	parityIndex, toolIndex := -1, -1
	for i, command := range plan.commands {
		switch {
		case strings.Contains(command.Script, ghHostsConfigRelativePath):
			parityIndex = i
		case strings.Contains(command.Script, "apt-get -s"):
			toolIndex = i
		}
	}
	if parityIndex == -1 {
		t.Fatal("gh token parity read missing without account-key access")
	}
	if toolIndex == -1 {
		t.Fatal("managed package read missing from plan")
	}
	if parityIndex > toolIndex {
		t.Fatalf("parity read at %d must precede tool apply at %d", parityIndex, toolIndex)
	}
	if !plan.allowsLive(lifecycle.GHTokenScript(cfg.Admin.Username)) {
		t.Fatal("deploy script is not a planned live command")
	}
}

func TestGHCredentialReadCommandFingerprintAndLiveness(t *testing.T) {
	fixture := newDoctorGitCommandFixture(t)
	fixture.writeExecutable(t, "sha256sum", "#!/bin/sh\nh=\"$(openssl dgst -sha256 -r | cut -d' ' -f1)\"\nprintf '%s  -\\n' \"$h\"\n")
	fixture.writeExecutable(t, "gh", "#!/bin/sh\n[ \"${GH_STUB_FAIL:-0}\" = 1 ] && exit 1\nexit 0\n")
	miseDir := filepath.Join(fixture.home, ".local", "bin")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mise := "#!/bin/sh\n[ \"$1\" = exec ] && shift\n[ \"$1\" = -- ] && shift\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(miseDir, "mise"), []byte(mise), 0o700); err != nil {
		t.Fatal(err)
	}
	token := "ghp_fixture_hosts_token"
	hosts := filepath.Join(fixture.home, filepath.FromSlash(ghHostsConfigRelativePath))
	writeHosts := func() {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(hosts), 0o700); err != nil {
			t.Fatal(err)
		}
		body := "github.com:\n    user: buzz\n    oauth_token: " + token + "\n    git_protocol: ssh\n"
		if err := os.WriteFile(hosts, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := ghCredentialReadCommand("deploy")
	wantSHA := "sha=" + credentialFingerprint(token)
	writeHosts()
	t.Setenv("GH_STUB_FAIL", "0")
	if got := fixture.runOutput(t, command); got != wantSHA+"\nauth=ok\n" {
		t.Fatalf("healthy credential read = %q, want %q", got, wantSHA+"\nauth=ok\n")
	}
	t.Setenv("GH_STUB_FAIL", "1")
	if got := fixture.runOutput(t, command); got != wantSHA+"\nauth=failed\n" {
		t.Fatalf("dead token read = %q", got)
	}
	if err := os.Remove(hosts); err != nil {
		t.Fatal(err)
	}
	if got := fixture.runOutput(t, command); got != "absent\nauth=failed\n" {
		t.Fatalf("absent hosts read = %q", got)
	}
}

func (f doctorGitCommandFixture) runOutput(t *testing.T, script string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "DOCTOR_GIT_HOME="+f.home, "PATH="+f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("command failed: %v: %s", err, out)
	}
	return string(out)
}
