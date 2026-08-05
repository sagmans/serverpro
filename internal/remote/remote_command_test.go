package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTailscaleSSHRequiresSudoPassword(t *testing.T) {
	_, err := TailscaleSSH{}.Run(context.Background(), "deploy", "prod-01", "true")
	if err == nil || !strings.Contains(err.Error(), "sudo password required") {
		t.Fatalf("expected sudo password error, got %v", err)
	}
}

func TestSudoSSHArgsDoNotContainPasswordOrScript(t *testing.T) {
	password := "correct horse battery staple"
	script := "printf '%s\\n' cloudflare-token"
	args := sudoSSHArgs("deploy", "prod-01")
	joined := strings.Join(args, " ")
	for _, leaked := range []string{password, script, "cloudflare-token"} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("argv leaked %q in %q", leaked, joined)
		}
	}
	payload := sudoPayloadWithInput(script, "", password)
	if !strings.Contains(payload, script) || !strings.Contains(payload, password) {
		t.Fatalf("stdin payload should carry script and password separately: %q", payload)
	}
	if !strings.HasPrefix(payload, "30\n") {
		t.Fatalf("payload should start with script byte count, got %q", payload)
	}
}

func TestRunTailscaleSSHExecutesCommandWithPayload(t *testing.T) {
	// WHY: the live path shells out to tailscale, but the local contract is still
	// testable: argv, stdin payload, default timeout, and combined output.
	dir := t.TempDir()
	path := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\nprintf 'args=%s\\n' \"$*\"\nprintf 'stdin='\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	out, err := runTailscaleSSH(context.Background(), 0, []string{"ssh", "deploy@prod-01", "true"}, "payload")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"args=ssh deploy@prod-01 true", "stdin=payload"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tailscale output %q missing %q", out, want)
		}
	}
}

func TestRunTailscaleSSHIncludesOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho nope >&2\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	out, err := runTailscaleSSH(context.Background(), time.Second, []string{"ssh", "deploy@prod-01"}, "")
	if err == nil || !strings.Contains(err.Error(), "tailscale ssh failed") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected wrapped tailscale failure, out=%q err=%v", out, err)
	}
	if !strings.Contains(out, "nope") {
		t.Fatalf("failure output = %q", out)
	}
}

func TestRemoteShellCommandPreservesMultilineScript(t *testing.T) {
	cmd := exec.Command("sh", "-c", remoteShellCommand("set -eu\nprintf '%s' ok"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("remote shell command failed: %v\n%s", err, out)
	}
	if string(out) != "ok" {
		t.Fatalf("remote shell command output = %q, want ok", out)
	}
}

func TestRemoteShellCommandQuotesBootstrapInSingleRemoteArg(t *testing.T) {
	args := sudoSSHArgs("deploy", "prod-01")
	if len(args) != 3 {
		t.Fatalf("sudo ssh args = %#v, want tailscale ssh target plus one remote command arg", args)
	}
	if !strings.HasPrefix(args[2], "sh -c '") {
		t.Fatalf("remote command not quoted as one shell program: %#v", args)
	}
	if strings.Contains(strings.Join(args, " "), "sh -c set -eu") {
		t.Fatalf("remote command can still split into bare set: %#v", args)
	}
}

func TestCommandOutputBufferWriteEnforcesLimit(t *testing.T) {
	unbounded := &commandOutputBuffer{}
	if n, err := unbounded.Write([]byte("open")); n != 4 || err != nil || unbounded.String() != "open" {
		t.Fatalf("unbounded write n=%d err=%v output=%q", n, err, unbounded.String())
	}

	bounded := &commandOutputBuffer{limit: 4}
	if n, err := bounded.Write([]byte("safe")); n != 4 || err != nil {
		t.Fatalf("within-limit write n=%d err=%v", n, err)
	}
	n, err := bounded.Write([]byte("overflow"))
	var limitErr *BatchOutputLimitError
	if n != len("overflow") || !errors.As(err, &limitErr) || bounded.String() != "safe" {
		t.Fatalf("overflow write n=%d err=%v output=%q", n, err, bounded.String())
	}
	if _, err := bounded.Write([]byte("again")); !errors.As(err, &limitErr) {
		t.Fatalf("repeated overflow error = %v", err)
	}
}
