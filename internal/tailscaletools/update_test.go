package tailscaletools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifestPinsApprovedRelease(t *testing.T) {
	if Version != "1.98.10" {
		t.Fatalf("tailscale version = %q, want 1.98.10", Version)
	}
	if AMD64SHA256 != "52490ce0832b245857e2afef7426d6ae5a4b49fb391412833cc95729bd23f7de" {
		t.Fatalf("amd64 digest = %q", AMD64SHA256)
	}
	if ARM64SHA256 != "d74a84e07cb1948d9f09a23ae161417c6127e562949773705c95d0762be2809d" {
		t.Fatalf("arm64 digest = %q", ARM64SHA256)
	}
	if RestartGrace < time.Second {
		t.Fatalf("restart grace = %s, want a delayed restart window", RestartGrace)
	}
}

func TestCheckCommandRequiresClientDaemonAndServiceVersion(t *testing.T) {
	command := CheckCommand()
	for _, want := range []string{
		"tailscale version --json",
		"tailscale status --json",
		`1.98.10`,
		"systemctl is-active tailscaled",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("check command missing %q: %s", want, command)
		}
	}
}

func TestUpdateScriptValidatesArchitectureBeforePackageMutation(t *testing.T) {
	script := UpdateScript()
	mainStart := strings.Index(script, "main() {")
	archCheck := strings.Index(script[mainStart:], "tailscale_arch >/dev/null")
	packageMutation := strings.Index(script[mainStart:], "install_prerequisites")
	if mainStart < 0 || archCheck < 0 || packageMutation < 0 || archCheck > packageMutation {
		t.Fatalf("architecture gate must precede package mutation in main")
	}
}

func TestUpdateScriptReexecutesBashThroughProductionShellDelivery(t *testing.T) {
	if _, err := exec.LookPath("dash"); err != nil {
		t.Skip("dash is required to mirror supported Ubuntu/Debian /bin/sh")
	}
	path := filepath.Join(t.TempDir(), "update.sh")
	if err := os.WriteFile(path, []byte(UpdateScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("dash", path)
	cmd.Env = append(os.Environ(), "SERVERPRO_TAILSCALE_SOURCE_ONLY=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("update script through production shell delivery: %v: %s", err, out)
	}
}

func TestUpdateScriptClearsPOSIXModeBeforeBashReexec(t *testing.T) {
	if _, err := exec.LookPath("dash"); err != nil {
		t.Skip("dash is required to mirror supported Ubuntu/Debian /bin/sh")
	}
	path := filepath.Join(t.TempDir(), "update.sh")
	if err := os.WriteFile(path, []byte(UpdateScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dash", path)
	cmd.Env = append(os.Environ(), "SERVERPRO_TAILSCALE_SOURCE_ONLY=1", "POSIXLY_CORRECT=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("POSIX mode caused interpreter re-exec loop: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("update script through POSIX shell delivery: %v: %s", err, out)
	}
}

func TestUpdateScriptPinsArtifactsAndDelaysDaemonRestart(t *testing.T) {
	script := UpdateScript()
	for _, want := range []string{
		"SERVERPRO_TAILSCALE_VERSION='1.98.10'",
		AMD64SHA256,
		ARM64SHA256,
		"https://pkgs.tailscale.com/stable/",
		"apt-get install -y --no-install-recommends ca-certificates curl jq",
		"sha256sum -c",
		"--no-same-owner",
		`GODEBUG="tlsmlkem=1"`,
		"systemd-run",
		"--on-active",
		"SERVERPRO_TAILSCALE_SOURCE_ONLY",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("update script missing %q", want)
		}
	}
	for _, forbidden := range []string{"https://tailscale.com/install.sh", "| sh"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("update script contains forbidden marker %q", forbidden)
		}
	}

	path := filepath.Join(t.TempDir(), "update.sh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("update script syntax: %v: %s", err, out)
	}
}
