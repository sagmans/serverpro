package remote

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/assagman/serverpro/internal/shell"
)

type Runner interface {
	Run(ctx context.Context, user, host, script string) (string, error)
}

type InputRunner interface {
	Runner
	RunWithInput(ctx context.Context, user, host, script, input string) (string, error)
}

type TailscaleSSH struct {
	Timeout      time.Duration
	SudoPassword string
}

func (r TailscaleSSH) Run(ctx context.Context, user, host, script string) (string, error) {
	return r.RunWithInput(ctx, user, host, script, "")
}

// WithTimeout returns a Runner whose per-call timeout is set to timeout when
// the underlying runner is a TailscaleSSH value or pointer. Other Runner
// implementations are returned unchanged because only TailscaleSSH exposes a
// Timeout field. The pointer form is cloned so the caller's original runner
// keeps its existing timeout.
func WithTimeout(r Runner, timeout time.Duration) Runner {
	switch runner := r.(type) {
	case TailscaleSSH:
		runner.Timeout = timeout
		return runner
	case *TailscaleSSH:
		clone := *runner
		clone.Timeout = timeout
		return clone
	default:
		return r
	}
}

func (r TailscaleSSH) RunWithInput(ctx context.Context, user, host, script, input string) (string, error) {
	if r.SudoPassword == "" {
		return "", fmt.Errorf("sudo password required")
	}
	return runTailscaleSSH(ctx, r.Timeout, sudoSSHArgs(user, host), sudoPayloadWithInput(script, input, r.SudoPassword))
}

func runTailscaleSSH(ctx context.Context, timeout time.Duration, args []string, stdin string) (string, error) {
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tailscale", args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("tailscale ssh failed: %w: %s", err, out.String())
	}
	return out.String(), nil
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
