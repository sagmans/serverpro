package doctor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/lifecycle"
)

const testGHPAT = "ghp_doctor_parity_fixture"

// curlStub proves the probe shape rather than the answer: the credential must
// arrive as a stdin config line and never as an argument, because argv is world
// readable on the managed host while the probe runs.
const curlStub = `#!/bin/sh
stdin_config=false
previous=""
for argument do
  if [ "$previous" = "-K" ] && [ "$argument" = "-" ]; then stdin_config=true; fi
  previous="$argument"
done
if [ "$stdin_config" != true ]; then printf '401'; exit 0; fi
config="$(cat)"
case "$*" in
  *"$CURL_STUB_TOKEN"*) echo 'token reached argv' >&2; exit 4 ;;
esac
case "$config" in
  *"Authorization: Bearer $CURL_STUB_TOKEN"*) ;;
  *) echo 'missing header on stdin' >&2; exit 3 ;;
esac
printf '%s' "${CURL_STUB_CODE:-}"
`

func TestGHTokenParityMatrix(t *testing.T) {
	readCommand := ghCredentialReadCommand("deploy")
	deployScript := lifecycle.GHTokenScript("deploy")
	localFingerprint := credentialFingerprint(testGHPAT)
	dead := "sha=" + localFingerprint + "\nauth=failed"
	ok := "sha=" + localFingerprint + "\nauth=ok"
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
		{name: "match", reads: []remoteCall{{out: ok}}, pat: testGHPAT, wantStatus: Pass, wantEvidence: "local=remote sha=" + localFingerprint},
		{name: "drift with local copy", reads: []remoteCall{{out: "sha=0123456789ab\nauth=ok"}}, pat: testGHPAT, wantStatus: Warn, wantEvidence: "differs from remote sha=0123456789ab", wantRemediation: ghTokenParityConvergeRemediation},
		{name: "drift without local copy", reads: []remoteCall{{out: "sha=0123456789ab\nauth=ok"}}, wantStatus: Warn, wantEvidence: "no PAT stored locally", wantRemediation: ghTokenParityConvergeRemediation},
		{name: "dead remote without fix", reads: []remoteCall{{out: dead}}, pat: testGHPAT, wantStatus: Warn, wantEvidence: "local sha=" + localFingerprint + " is available", wantRemediation: ghTokenParityDeployHint},
		{name: "dead remote deploys under fix", reads: []remoteCall{{out: dead}, {out: ok}}, deploy: []remoteCall{{out: "gh authenticated as buzz"}}, pat: testGHPAT, fix: true, wantStatus: Pass, wantEvidence: "fixed: deployed local sha=" + localFingerprint, wantDeploy: true},
		{name: "unanswered probe never deploys", reads: []remoteCall{{out: "sha=0123456789ab\nauth=unknown"}}, pat: testGHPAT, fix: true, wantStatus: Warn, wantEvidence: "GitHub did not answer the credential probe", wantRemediation: ghTokenParityUnverifiedRemediation},
		{name: "unanswered probe leaves matching token alone", reads: []remoteCall{{out: "sha=" + localFingerprint + "\nauth=unknown"}}, pat: testGHPAT, fix: true, wantStatus: Warn, wantEvidence: "local sha=" + localFingerprint, wantRemediation: ghTokenParityUnverifiedRemediation},
		{name: "silent probe never deploys", reads: []remoteCall{{out: "sha=0123456789ab"}}, pat: testGHPAT, fix: true, wantStatus: Warn, wantEvidence: "remote sha=0123456789ab", wantRemediation: ghTokenParityUnverifiedRemediation},
		{name: "rejected deploy recommends rotation", reads: []remoteCall{{out: dead}}, deploy: []remoteCall{{err: errors.New("GitHub PAT validation failed")}}, pat: testGHPAT, fix: true, wantStatus: Fail, wantEvidence: "deploying the stored PAT failed", wantRemediation: ghTokenParityRotateRemediation, wantDeploy: true},
		{name: "recheck rejects deployed copy", reads: []remoteCall{{out: dead}, {out: dead}}, deploy: []remoteCall{{out: "gh authenticated as buzz"}}, pat: testGHPAT, fix: true, wantStatus: Fail, wantEvidence: "GitHub rejected the stored PAT that was just deployed", wantRemediation: ghTokenParityRotateRemediation, wantDeploy: true},
		{name: "recheck lost the deployed copy", reads: []remoteCall{{out: dead}, {out: "sha=0123456789ab\nauth=failed"}}, deploy: []remoteCall{{out: "gh authenticated as buzz"}}, pat: testGHPAT, fix: true, wantStatus: Fail, wantEvidence: "remote hosts.yml does not hold it", wantRemediation: ghTokenParityRetryRemediation, wantDeploy: true},
		{name: "recheck unanswered", reads: []remoteCall{{out: dead}, {out: "sha=" + localFingerprint + "\nauth=unknown"}}, deploy: []remoteCall{{out: "gh authenticated as buzz"}}, pat: testGHPAT, fix: true, wantStatus: Fail, wantEvidence: "GitHub did not answer the recheck", wantRemediation: ghTokenParityRetryRemediation, wantDeploy: true},
		{name: "dead remote without local copy", reads: []remoteCall{{out: "sha=0123456789ab\nauth=failed"}}, wantStatus: Warn, wantEvidence: "no PAT is stored locally", wantRemediation: ghTokenParityRotateRemediation},
		{name: "nothing stored anywhere", reads: []remoteCall{{out: "sha=absent\nauth=absent"}}, wantStatus: Skip, wantEvidence: "no github PAT stored"},
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

// TestGHCredentialReadCommandFingerprintAndVerdict pins the remote protocol:
// GitHub's answer decides the credential verdict, the toolchain never does, and
// an answer that never arrives stays unknown instead of unproven.
func TestGHCredentialReadCommandFingerprintAndVerdict(t *testing.T) {
	fixture := newDoctorGitCommandFixture(t)
	fixture.writeExecutable(t, "sha256sum", "#!/bin/sh\nh=\"$(openssl dgst -sha256 -r | cut -d' ' -f1)\"\nprintf '%s  -\\n' \"$h\"\n")
	fixture.writeExecutable(t, "curl", curlStub)
	token := "ghp_fixture_hosts_token"
	hosts := filepath.Join(fixture.home, filepath.FromSlash(ghHostsConfigRelativePath))
	writeHosts := func(t *testing.T, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(hosts), 0o700); err != nil {
			t.Fatal(err)
		}
		body := "github.com:\n    user: buzz\n    oauth_token: " + value + "\n    git_protocol: ssh\n"
		if err := os.WriteFile(hosts, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := ghCredentialReadCommand("deploy")
	wantSHA := "sha=" + credentialFingerprint(token) + "\n"
	t.Setenv("CURL_STUB_TOKEN", token)
	writeHosts(t, token)
	for _, tc := range []struct {
		name string
		code string
		want string
	}{
		{name: "GitHub accepts", code: "200", want: wantSHA + "auth=ok\n"},
		{name: "GitHub rejects", code: "401", want: wantSHA + "auth=failed\n"},
		{name: "probe unanswered", code: "503", want: wantSHA + "auth=unknown\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CURL_STUB_CODE", tc.code)
			if got := fixture.runOutput(t, command); got != tc.want {
				t.Fatalf("read = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("unquotable token stays unproven", func(t *testing.T) {
		value := token + `"`
		writeHosts(t, value)
		t.Setenv("CURL_STUB_CODE", "200")
		want := "sha=" + credentialFingerprint(value) + "\nauth=unknown\n"
		if got := fixture.runOutput(t, command); got != want {
			t.Fatalf("read = %q, want %q", got, want)
		}
	})
	t.Run("no remote token", func(t *testing.T) {
		if err := os.Remove(hosts); err != nil {
			t.Fatal(err)
		}
		if got := fixture.runOutput(t, command); got != "sha=absent\nauth=absent\n" {
			t.Fatalf("read = %q", got)
		}
	})
}

// curlLoopbackWrapper isolates GitHub traffic while preserving real curl argument and stdin handling.
const curlLoopbackWrapper = `#!/bin/sh
for argument do
  shift
  case "$argument" in
    *"$CURL_STUB_TOKEN"*) echo 'token reached argv' >&2; exit 4 ;;
  esac
  case "$argument" in
    https://api.github.com/user) set -- "$@" "$CURL_TEST_URL" ;;
    *) set -- "$@" "$argument" ;;
  esac
done
exec "$CURL_TEST_BINARY" --noproxy '*' "$@"
`

// TestGHCredentialReadCommandRealCurl prevents permissive stubs from hiding unauthenticated requests.
func TestGHCredentialReadCommandRealCurl(t *testing.T) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl unavailable for local HTTP regression")
	}
	const token = "ghp_loopback_fixture_token"
	headers := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		headers <- header
		if header != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	fixture := newDoctorGitCommandFixture(t)
	fixture.writeExecutable(t, "sha256sum", "#!/bin/sh\nh=\"$(openssl dgst -sha256 -r | cut -d' ' -f1)\"\nprintf '%s  -\\n' \"$h\"\n")
	fixture.writeExecutable(t, "curl", curlLoopbackWrapper)
	hosts := filepath.Join(fixture.home, filepath.FromSlash(ghHostsConfigRelativePath))
	if err := os.MkdirAll(filepath.Dir(hosts), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "github.com:\n    user: fixture\n    oauth_token: " + token + "\n    git_protocol: ssh\n"
	if err := os.WriteFile(hosts, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURL_STUB_TOKEN", token)
	t.Setenv("CURL_TEST_BINARY", curl)
	t.Setenv("CURL_TEST_URL", server.URL)
	got := fixture.runOutput(t, ghCredentialReadCommand("deploy"))
	want := "sha=" + credentialFingerprint(token) + "\nauth=ok\n"
	if got != want {
		t.Fatalf("read = %q, want %q", got, want)
	}
	select {
	case header := <-headers:
		if header != "Bearer "+token {
			t.Fatal("local endpoint did not receive the expected Authorization header")
		}
	default:
		t.Fatal("credential probe did not reach the local endpoint")
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
