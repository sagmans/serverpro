package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/mesh"
)

// Fixture values shared by the automation tests. They mirror the shapes that
// tailscaled reports, so a matcher regression shows up as a test failure and
// not as a session sent to the wrong server.
const (
	testAutomationSocket     = "/var/run/serverpro-automation/tailscaled.sock"
	testAutomationUser       = "deploy"
	testAutomationHostBase   = "prod-01"
	testAutomationHostFQDN   = "prod-01.tail1234.ts.net"
	testAutomationHostIPv4   = "100.64.0.7"
	testAutomationHostIPv6   = "fd7a:115c:a1e0::7"
	testAutomationOtherFQDN  = "prod-02.tail1234.ts.net"
	testAutomationOtherIPv4  = "100.64.0.8"
	testAutomationTwinFQDN   = "prod-01.tail9999.ts.net"
	testAutomationTwinIPv4   = "100.64.0.9"
	testAutomationHostKeyEd  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestEd25519Key"
	testAutomationHostKeyRSA = "ssh-rsa AAAAB3NzaC1yc2EAAAADTestRsaKey"
	// testAutomationDecoyKey belongs to a same-name peer without the server
	// tag; seeing it in known_hosts would mean the wrong peer was trusted.
	testAutomationDecoyKey    = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDecoyKey"
	testAutomationServerTag   = "tag:serverpro"
	testAutomationForeignTag  = "tag:shared-in"
	testAutomationOperatorTag = "tag:operator"
	testAutomationSelfFQDN    = "operator-automation.tail1234.ts.net."
	testAutomationRemoteCmd   = "true"
	testAutomationStdin       = "payload-for-remote"
	testAutomationSSHTimeout  = 10 * time.Second
	testAutomationCLIStderr   = "failed to connect to local tailscaled"
	testAutomationCLIFailCode = 3
	// testAutomationCaptureEnv tells the fake ssh where to drop what it saw,
	// so the test can inspect argv, stdin and the known_hosts file after the
	// real code has already removed its temp directory.
	testAutomationCaptureEnv = "SERVERPRO_TEST_AUTOMATION_CAPTURE"
)

// fakeAutomationSSH stands in for OpenSSH. It copies the known_hosts file
// that serverpro wrote, because that file must be gone once run returns.
const fakeAutomationSSH = `#!/bin/sh
set -eu
capture="${` + testAutomationCaptureEnv + `:?}"
: > "$capture/args"
known=""
prev=""
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$capture/args"
  if [ "$prev" = "-o" ]; then
    case "$arg" in UserKnownHostsFile=*) known="${arg#UserKnownHostsFile=}" ;; esac
  fi
  prev="$arg"
done
printf '%s' "$known" > "$capture/known_hosts_path"
cp "$known" "$capture/known_hosts"
cat > "$capture/stdin"
printf 'ssh-ok\n'
`

// testAutomationPeerJSON builds one peer with the JSON keys that tailscale
// really emits. Raw keys, not the Go struct, keep the json tags honest.
func testAutomationPeerJSON(dnsName string, ips, tags, hostKeys []string) map[string]any {
	return map[string]any{
		"DNSName":      dnsName,
		"TailscaleIPs": ips,
		"Tags":         tags,
		"sshHostKeys":  hostKeys,
	}
}

func testAutomationStatusJSON(t *testing.T, backendState string, selfTags []string, peers ...map[string]any) []byte {
	t.Helper()
	peerMap := map[string]any{}
	for i, peer := range peers {
		peerMap["nodekey:"+strconv.Itoa(i)] = peer
	}
	out, err := json.Marshal(map[string]any{
		"BackendState": backendState,
		"Self":         map[string]any{"DNSName": testAutomationSelfFQDN, "Tags": selfTags},
		"Peer":         peerMap,
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// testTargetPeerJSON is the one server peer that a ready netmap must yield.
func testTargetPeerJSON() map[string]any {
	return testAutomationPeerJSON(testAutomationHostFQDN+".", []string{testAutomationHostIPv4, testAutomationHostIPv6},
		[]string{testAutomationServerTag}, []string{testAutomationHostKeyEd, testAutomationHostKeyRSA})
}

// testDecoyPeerJSON shares the target's base name but not its server tag,
// like a node shared in from another tailnet.
func testDecoyPeerJSON() map[string]any {
	return testAutomationPeerJSON(testAutomationTwinFQDN+".", []string{testAutomationTwinIPv4},
		[]string{testAutomationForeignTag}, []string{testAutomationDecoyKey})
}

func testReadyAutomationStatusJSON(t *testing.T) []byte {
	t.Helper()
	return testAutomationStatusJSON(t, automationBackendRunning, []string{mesh.AutomationSourceTag},
		testAutomationPeerJSON(testAutomationOtherFQDN+".", []string{testAutomationOtherIPv4}, []string{testAutomationServerTag}, []string{testAutomationHostKeyEd}),
		testDecoyPeerJSON(),
		testTargetPeerJSON(),
	)
}

// fakeAutomationCLI is what the tailscale stand-in answers to "status".
type fakeAutomationCLI struct {
	status   []byte
	stderr   string
	exitCode int
}

// install writes a tailscale stand-in that answers "status --json" from a
// file. It records argv so the test proves the call targets the tagged
// node's socket and never the default tailscaled.
func (c fakeAutomationCLI) install(t *testing.T, dir string) string {
	t.Helper()
	statusPath := filepath.Join(dir, "status.json")
	if err := os.WriteFile(statusPath, c.status, 0o600); err != nil {
		t.Fatal(err)
	}
	stderrPath := filepath.Join(dir, "stderr.txt")
	if err := os.WriteFile(stderrPath, []byte(c.stderr), 0o600); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(dir, "tailscale")
	script := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$*\" > " + testShellQuote(filepath.Join(dir, "cli_args")) +
		"\ncat " + testShellQuote(stderrPath) + " >&2" +
		"\ncat " + testShellQuote(statusPath) +
		"\nexit " + strconv.Itoa(c.exitCode) + "\n"
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return cli
}

// automationFixture hands the fake ssh to the node by path and points TMPDIR
// at a test directory, so no real ssh, tailscaled or shared /tmp is touched.
type automationFixture struct {
	node    AutomationNode
	cliDir  string
	capture string
	tmpDir  string
}

func newAutomationFixture(t *testing.T, cli fakeAutomationCLI) automationFixture {
	t.Helper()
	cliDir := t.TempDir()
	sshDir := t.TempDir()
	capture := t.TempDir()
	tmpDir := t.TempDir()
	sshPath := filepath.Join(sshDir, "ssh")
	if err := os.WriteFile(sshPath, []byte(fakeAutomationSSH), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmpDir)
	t.Setenv(testAutomationCaptureEnv, capture)
	return automationFixture{
		node: AutomationNode{
			CLI:    cli.install(t, cliDir),
			Socket: testAutomationSocket,
			Tags:   []string{testAutomationServerTag},
			SSH:    sshPath,
		},
		cliDir:  cliDir,
		capture: capture,
		tmpDir:  tmpDir,
	}
}

func (f automationFixture) run(t *testing.T, host string) (string, error) {
	t.Helper()
	return f.node.run(context.Background(), testAutomationSSHTimeout, testAutomationUser, host, testAutomationRemoteCmd, testAutomationStdin, 0)
}

// read returns one artifact that the fake ssh captured.
func (f automationFixture) read(t *testing.T, name string) string {
	t.Helper()
	return readFile(t, filepath.Join(f.capture, name))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestAutomationNodeRunPinsSSHToTaggedNode(t *testing.T) {
	// WHY: this is the whole security contract of the transport. Each option
	// below closes one way for the session to bypass the tagged node, to
	// trust an unverified host key, or to leak the operator's identities.
	f := newAutomationFixture(t, fakeAutomationCLI{status: testReadyAutomationStatusJSON(t)})
	out, err := f.run(t, testAutomationHostBase)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ssh-ok\n" {
		t.Fatalf("run output = %q", out)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(f.cliDir, "cli_args"))); got != "--socket="+testAutomationSocket+" status --json" {
		t.Fatalf("status argv = %q", got)
	}

	knownHostsPath := f.read(t, "known_hosts_path")
	wantArgs := []string{
		"-F", "/dev/null",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + knownHostsPath,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "UpdateHostKeys=no",
		"-o", "CanonicalizeHostname=no",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "PubkeyAuthentication=no",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ProxyCommand=" + f.node.CLI + " --socket=" + testAutomationSocket + " nc %h %p",
		"--",
		testAutomationUser + "@" + testAutomationHostFQDN,
		testAutomationRemoteCmd,
	}
	if got := strings.Split(strings.TrimSuffix(f.read(t, "args"), "\n"), "\n"); !slices.Equal(got, wantArgs) {
		t.Fatalf("ssh argv mismatch\n got: %#v\nwant: %#v", got, wantArgs)
	}

	// The decoy shares the base name, so only its missing server tag keeps
	// its key out of this file.
	wantKnownHosts := testAutomationHostFQDN + " " + testAutomationHostKeyEd + "\n" + testAutomationHostFQDN + " " + testAutomationHostKeyRSA + "\n"
	if got := f.read(t, "known_hosts"); got != wantKnownHosts {
		t.Fatalf("known_hosts = %q, want %q", got, wantKnownHosts)
	}
	if got := f.read(t, "stdin"); got != testAutomationStdin {
		t.Fatalf("ssh stdin = %q, want %q", got, testAutomationStdin)
	}

	// WHY: a known_hosts file left behind could vouch for a host in a later
	// run, so the private directory must be gone on return.
	if !strings.HasPrefix(knownHostsPath, f.tmpDir) || filepath.Base(knownHostsPath) != knownHostsFileName {
		t.Fatalf("known_hosts path %q not in private temp dir under %q", knownHostsPath, f.tmpDir)
	}
	if _, err := os.Stat(filepath.Dir(knownHostsPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("known_hosts dir still present after run: %v", err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(f.tmpDir, knownHostsDirPattern+"*")); len(leftovers) != 0 {
		t.Fatalf("temp dirs left behind: %v", leftovers)
	}
}

func TestAutomationSSHClientDefaultIsAbsolute(t *testing.T) {
	// WHY: the client gets the sudo password on stdin, so the default must
	// never resolve through PATH, where any user-writable entry could win.
	if !filepath.IsAbs(automationSSHClient) {
		t.Fatalf("default ssh client %q is not an absolute path", automationSSHClient)
	}
}

func TestAutomationNodeRunFailsClosedWhenNotReady(t *testing.T) {
	// WHY: every not-ready state must stop before ssh starts. The two
	// sentinels must stay apart: lifecycle stops at once on a node fault but
	// waits out a peer that is still joining the netmap.
	readySelf := []string{mesh.AutomationSourceTag}
	tests := []struct {
		name     string
		cli      fakeAutomationCLI
		tags     []string
		host     string
		wantErr  error
		wantText string
	}{
		{
			name:     "status command fails",
			cli:      fakeAutomationCLI{status: []byte("{}"), stderr: "  " + testAutomationCLIStderr + "\n", exitCode: testAutomationCLIFailCode},
			wantErr:  ErrAutomationNodeNotReady,
			wantText: "status via socket " + testAutomationSocket + ": exit status " + strconv.Itoa(testAutomationCLIFailCode) + ": " + testAutomationCLIStderr,
		},
		{
			name:     "status command fails silently",
			cli:      fakeAutomationCLI{status: []byte("{}"), exitCode: testAutomationCLIFailCode},
			wantErr:  ErrAutomationNodeNotReady,
			wantText: "status via socket",
		},
		{
			name:     "status is not JSON",
			cli:      fakeAutomationCLI{status: []byte("not json")},
			wantErr:  ErrAutomationNodeNotReady,
			wantText: "status JSON via socket",
		},
		{
			name:     "backend not running",
			cli:      fakeAutomationCLI{status: testAutomationStatusJSON(t, "Stopped", readySelf, testTargetPeerJSON())},
			wantErr:  ErrAutomationNodeNotReady,
			wantText: "backend state",
		},
		{
			name:     "node lacks automation tag",
			cli:      fakeAutomationCLI{status: testAutomationStatusJSON(t, automationBackendRunning, []string{testAutomationOperatorTag}, testTargetPeerJSON())},
			wantErr:  ErrAutomationNodeNotReady,
			wantText: "do not include",
		},
		{
			name:     "no server tags to check against",
			cli:      fakeAutomationCLI{status: testReadyAutomationStatusJSON(t)},
			tags:     []string{},
			wantErr:  ErrAutomationNodeNotReady,
			wantText: "no server tags",
		},
		{
			name: "two tagged peers match",
			cli: fakeAutomationCLI{status: testAutomationStatusJSON(t, automationBackendRunning, readySelf, testTargetPeerJSON(),
				testAutomationPeerJSON(testAutomationTwinFQDN+".", []string{testAutomationTwinIPv4}, []string{testAutomationServerTag}, []string{testAutomationDecoyKey}))},
			wantErr:  ErrAutomationNodeNotReady,
			wantText: "2 peers",
		},
		{
			name:     "peer not in netmap",
			cli:      fakeAutomationCLI{status: testReadyAutomationStatusJSON(t)},
			host:     "prod-99",
			wantErr:  ErrAutomationPeerNotReady,
			wantText: "in the automation node netmap",
		},
		{
			name:     "only an untagged peer matches the name",
			cli:      fakeAutomationCLI{status: testAutomationStatusJSON(t, automationBackendRunning, readySelf, testDecoyPeerJSON())},
			wantErr:  ErrAutomationPeerNotReady,
			wantText: "in the automation node netmap",
		},
		{
			name: "peer has no host keys",
			cli: fakeAutomationCLI{status: testAutomationStatusJSON(t, automationBackendRunning, readySelf,
				testAutomationPeerJSON(testAutomationHostFQDN+".", []string{testAutomationHostIPv4}, []string{testAutomationServerTag}, nil))},
			wantErr:  ErrAutomationPeerNotReady,
			wantText: "no Tailscale SSH host keys",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutomationFixture(t, tt.cli)
			if tt.tags != nil {
				f.node.Tags = tt.tags
			}
			host := tt.host
			if host == "" {
				host = testAutomationHostBase
			}
			_, err := f.run(t, host)
			if !errors.Is(err, tt.wantErr) || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("err = %v, want %v containing %q", err, tt.wantErr, tt.wantText)
			}
			other := ErrAutomationPeerNotReady
			if errors.Is(tt.wantErr, ErrAutomationPeerNotReady) {
				other = ErrAutomationNodeNotReady
			}
			if errors.Is(err, other) {
				t.Fatalf("err = %v also matches %v; retry policy would misfire", err, other)
			}
			if _, statErr := os.Stat(filepath.Join(f.capture, "args")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("ssh ran although the target was not ready: %v", statErr)
			}
		})
	}
}

func TestAutomationNodeStatusCapsCLIStderr(t *testing.T) {
	// WHY: a runaway CLI must not flood the error that create logs and
	// shows, yet the head of its stderr still has to explain the failure.
	overflow := 64
	stderr := strings.Repeat("e", automationStderrLimit+overflow)
	f := newAutomationFixture(t, fakeAutomationCLI{status: []byte("{}"), stderr: stderr, exitCode: testAutomationCLIFailCode})
	_, err := f.node.status(context.Background())
	if !errors.Is(err, ErrAutomationNodeNotReady) {
		t.Fatalf("err = %v, want %v", err, ErrAutomationNodeNotReady)
	}
	if !strings.HasSuffix(err.Error(), ": "+stderr[:automationStderrLimit]) || strings.Contains(err.Error(), stderr[:automationStderrLimit+1]) {
		t.Fatalf("stderr not capped at %d bytes: %q", automationStderrLimit, err)
	}
}

func TestAutomationPeerMatchesStoredHostForms(t *testing.T) {
	// WHY: state stores a server by FQDN, short name or Tailscale IP. Each
	// form must find the same peer, and a near miss must never match.
	peer := automationPeer{
		DNSName:      testAutomationHostFQDN + ".",
		TailscaleIPs: []string{testAutomationHostIPv4, testAutomationHostIPv6},
	}
	tests := []struct {
		host string
		want bool
	}{
		{testAutomationHostFQDN, true},
		{testAutomationHostFQDN + ".", true},
		{strings.ToUpper(testAutomationHostFQDN), true},
		{testAutomationHostBase, true},
		{testAutomationHostIPv4, true},
		{testAutomationHostIPv6, true},
		// Non-canonical IPv6 must still match after normalization.
		{"fd7a:115c:a1e0:0:0:0:0:7", true},
		{"", false},
		{testAutomationOtherIPv4, false},
		{testAutomationOtherFQDN, false},
		{testAutomationTwinFQDN, false},
		{"prod", false},
		{testAutomationHostBase + ".other.example", false},
	}
	for _, tt := range tests {
		if got := peer.matches(tt.host); got != tt.want {
			t.Errorf("matches(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
	// A peer without a MagicDNS suffix has no base label to fall back on.
	if !(automationPeer{DNSName: testAutomationHostBase}).matches(testAutomationHostBase) {
		t.Error("bare peer name did not match itself")
	}
}

func TestAutomationPeerKnownHostsDropsUnsafeKeys(t *testing.T) {
	// WHY: a key with an embedded newline could inject a second known_hosts
	// line that vouches for another host, so such keys must be dropped.
	peer := automationPeer{
		DNSName: testAutomationHostFQDN + ".",
		SSHHostKeys: []string{
			"  " + testAutomationHostKeyEd + "  ",
			"",
			"   ",
			testAutomationHostKeyRSA + "\n" + testAutomationOtherFQDN + " " + testAutomationHostKeyEd,
			testAutomationHostKeyRSA + "\r",
		},
	}
	want := testAutomationHostFQDN + " " + testAutomationHostKeyEd + "\n" + testAutomationHostFQDN + " " + testAutomationHostKeyRSA + "\n"
	if got := string(peer.knownHosts()); got != want {
		t.Fatalf("knownHosts = %q, want %q", got, want)
	}
}
