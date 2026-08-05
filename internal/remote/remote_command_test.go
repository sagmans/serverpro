package remote

import (
	"context"
	"os/exec"
	"strings"
	"testing"
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
