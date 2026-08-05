package remote

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/shell"
)

type Runner interface {
	Run(ctx context.Context, user, host, script string) (string, error)
}

type InputRunner interface {
	Runner
	RunWithInput(ctx context.Context, user, host, script, input string) (string, error)
}

const defaultTailscaleSSHTimeout = 2 * time.Minute

type TailscaleSSH struct {
	Timeout      time.Duration
	SudoPassword string
}

func (r TailscaleSSH) Run(ctx context.Context, user, host, script string) (string, error) {
	return r.RunWithInput(ctx, user, host, script, "")
}

type timeoutRunner struct {
	runner  Runner
	timeout time.Duration
}

// WithTimeout applies one caller-visible deadline regardless of runner type.
func WithTimeout(r Runner, timeout time.Duration) Runner {
	return timeoutRunner{runner: r, timeout: timeout}
}

func (r timeoutRunner) Run(ctx context.Context, user, host, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.runner.Run(ctx, user, host, script)
}

func (r TailscaleSSH) RunWithInput(ctx context.Context, user, host, script, input string) (string, error) {
	if r.SudoPassword == "" {
		return "", fmt.Errorf("sudo password required")
	}
	return r.runWithInputLimit(ctx, user, host, script, input, 0)
}

func (r TailscaleSSH) runWithInputLimit(ctx context.Context, user, host, script, input string, outputLimit int) (string, error) {
	return runTailscaleSSHWithOutputLimit(ctx, r.Timeout, sudoSSHArgs(user, host), sudoPayloadWithInput(script, input, r.SudoPassword), outputLimit)
}

func runTailscaleSSH(ctx context.Context, timeout time.Duration, args []string, stdin string) (string, error) {
	return runTailscaleSSHWithOutputLimit(ctx, timeout, args, stdin, 0)
}

func runTailscaleSSHWithOutputLimit(ctx context.Context, timeout time.Duration, args []string, stdin string, outputLimit int) (string, error) {
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = contextWithDefaultTimeout(ctx, defaultTailscaleSSHTimeout)
	}
	defer cancel()
	cmd := exec.CommandContext(ctx, "tailscale", args...)
	cmd.Stdin = strings.NewReader(stdin)
	out := &commandOutputBuffer{limit: outputLimit}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	if out.limitErr != nil {
		return "", out.limitErr
	}
	if err != nil {
		return out.String(), fmt.Errorf("tailscale ssh failed: %w: %s", err, out.String())
	}
	return out.String(), nil
}

type commandOutputBuffer struct {
	bytes.Buffer
	limit    int
	limitErr error
}

func (b *commandOutputBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.Buffer.Write(p)
	}
	if b.limitErr != nil {
		return len(p), b.limitErr
	}
	remaining := b.limit - b.Len()
	if len(p) <= remaining {
		return b.Buffer.Write(p)
	}
	if remaining > 0 {
		_, _ = b.Buffer.Write(p[:remaining])
	}
	// WHY: stop pipe readers immediately so hostile remote output cannot grow
	// the local process after the retained evidence reaches its fixed ceiling.
	b.limitErr = &BatchOutputLimitError{Limit: b.limit}
	return len(p), b.limitErr
}

func contextWithDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func sudoSSHArgs(user, host string) []string {
	return []string{"ssh", fmt.Sprintf("%s@%s", user, host), remoteShellCommand(sudoBootstrapCommand())}
}

func remoteShellCommand(script string) string {
	return "sh -c " + shell.Quote(script)
}

func sudoPayloadWithInput(script, input, password string) string {
	return fmt.Sprintf("%d\n%s\n%s\n%d\n%s", len(script), script, password, len(input), input)
}

func sudoBootstrapCommand() string {
	return receiveScriptBootstrap() + `
IFS= read -r sudo_password
printf '%s\n' "$sudo_password" | sudo -S -p '' -v
run_script_with_payload_input`
}

func receiveScriptBootstrap() string {
	return `set -eu
script_tmp="$(mktemp)"
cleanup() { rm -f "$script_tmp"; }
trap cleanup EXIT
chmod 600 "$script_tmp"
read_size() {
  IFS= read -r value
  case "$value" in ''|*[!0-9]*) echo 'invalid stdin payload' >&2; exit 125 ;; esac
  printf '%s' "$value"
}
run_script_with_payload_input() {
  input_size="$(read_size)"
  if [ "$input_size" -gt 0 ]; then
    dd bs=1 count="$input_size" 2>/dev/null | sudo -n sh "$script_tmp"
  else
    sudo -n sh "$script_tmp" </dev/null
  fi
}
script_size="$(read_size)"
dd bs=1 count="$script_size" of="$script_tmp" 2>/dev/null
IFS= read -r _script_separator`
}
