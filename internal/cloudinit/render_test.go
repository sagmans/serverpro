package cloudinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/shell"
	"gopkg.in/yaml.v3"
)

const testAdminPasswordHash = "$6$rounds=100000$abcdefghijklmnop$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestRenderHardeningAndTailscale(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PermitRootLogin no", "PasswordAuthentication no", "KbdInteractiveAuthentication no", "ChallengeResponseAuthentication no", "X11Forwarding no", "AllowAgentForwarding no", "AllowTcpForwarding no", "PermitTunnel no", "PermitOpen none", "ufw default deny incoming", "tailscale up", "/root/.serverpro-tailscale-authkey", "--hostname 'prod-01'", "--ssh", "groups: sudo", "hashed_passwd: " + testAdminPasswordHash} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud-init missing %q\n%s", want, out)
		}
	}
	for _, want := range []string{"grep -Eq '^[[:space:]]*#?[[:space:]]*PermitRootLogin[[:space:]]+' /etc/ssh/sshd_config", "sed -i -E 's/^[[:space:]]*#?[[:space:]]*PermitRootLogin[[:space:]]+.*/PermitRootLogin no/' /etc/ssh/sshd_config", "printf 'PermitRootLogin no\\n' > \"$tmp\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud-init should normalize main sshd_config with %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "trap cleanup EXIT") {
		t.Fatalf("cloud-init should clean bootstrap secrets with an EXIT trap\n%s", out)
	}
	if strings.Contains(out, "&& rm -f") {
		t.Fatalf("cloud-init cleanup must not depend on tailscale success\n%s", out)
	}
	if strings.Contains(out, "[ sh, -c") {
		t.Fatalf("cloud-init should avoid fragile flow-style shell commands\n%s", out)
	}
	for _, removed := range []string{"NOPASSWD", "lock_passwd: true", "chpasswd:", "\n    passwd: "} {
		if strings.Contains(out, removed) {
			t.Fatalf("cloud-init should not render %q\n%s", removed, out)
		}
	}
	if err := yaml.Unmarshal([]byte(out), new(map[string]any)); err != nil {
		t.Fatalf("cloud-init rendered invalid YAML: %v\n%s", err, out)
	}
	for _, secret := range []string{"hetzner-secret-token", "tailscale-api-token", "cf-token"} {
		if strings.Contains(out, secret) {
			t.Fatalf("cloud-init leaked %q", secret)
		}
	}
}

func TestRenderPinsAndVerifiesTailscaleArtifacts(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"TAILSCALE_VERSION=1.98.10",
		"52490ce0832b245857e2afef7426d6ae5a4b49fb391412833cc95729bd23f7de",
		"d74a84e07cb1948d9f09a23ae161417c6127e562949773705c95d0762be2809d",
		"tailscale_${TAILSCALE_VERSION}_${arch}.tgz",
		"sha256sum -c",
		"--no-same-owner",
		"GODEBUG=\"tlsmlkem=1\"",
		"systemctl enable --now tailscaled",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud-init missing pinned Tailscale control %q\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"https://tailscale.com/install.sh", "| sh"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("cloud-init retains unpinned installer %q\n%s", forbidden, out)
		}
	}
}

func TestRenderedTailscaleInstallerRejectsUnsupportedArchitecture(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	installer := extractTailscaleInstaller(t, out)
	if !strings.Contains(installer, "SERVERPRO_TAILSCALE_SOURCE_ONLY") {
		t.Fatalf("installer needs source-only guard before executable helper testing\n%s", installer)
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(installer), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCloudInitFakeCommand(t, binDir, "uname", "printf 'sparc64\\n'\n")
	curlMarker := filepath.Join(dir, "curl-ran")
	writeCloudInitFakeCommand(t, binDir, "curl", "printf ran > \"$CURL_MARKER\"\n")

	cmd := exec.Command("sh", "-c", `. "$1"; install_tailscale`, "test", scriptPath)
	cmd.Env = append(os.Environ(), "SERVERPRO_TAILSCALE_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"), "CURL_MARKER="+curlMarker)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "unsupported Tailscale architecture") {
		t.Fatalf("unsupported architecture should fail closed: err=%v output=%s", err, output)
	}
	if _, err := os.Stat(curlMarker); !os.IsNotExist(err) {
		t.Fatalf("unsupported architecture reached download: %v", err)
	}
}

func TestRenderedTailscaleInstallerExecutableMatrix(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	installer := extractTailscaleInstaller(t, out)
	cases := []struct {
		name         string
		machine      string
		archiveArch  string
		checksum     string
		checksumFail bool
	}{
		{name: "amd64", machine: "x86_64", archiveArch: "amd64", checksum: tailscaleAMD64SHA256},
		{name: "arm64", machine: "aarch64", archiveArch: "arm64", checksum: tailscaleARM64SHA256},
		{name: "checksum-mismatch", machine: "x86_64", archiveArch: "amd64", checksum: tailscaleAMD64SHA256, checksumFail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			scriptPath := filepath.Join(dir, "install.sh")
			if err := os.WriteFile(scriptPath, []byte(installer), 0o600); err != nil {
				t.Fatal(err)
			}
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			writeCloudInitFakeCommand(t, binDir, "uname", "printf '%s\\n' \"$TEST_MACHINE\"\n")
			writeCloudInitFakeCommand(t, binDir, "curl", "for arg; do output=$arg; done\nprintf archive > \"$output\"\n")
			writeCloudInitFakeCommand(t, binDir, "sha256sum", "grep -F \"$EXPECTED_CHECKSUM\" checksums >/dev/null\n[ \"$CHECKSUM_FAIL\" = 0 ]\n")
			writeCloudInitFakeCommand(t, binDir, "tar", `directory=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --directory ]; then
    shift
    directory=$1
  fi
  shift
done
mkdir -p "$directory/$TEST_ROOT/systemd"
touch "$directory/$TEST_ROOT/tailscale" "$directory/$TEST_ROOT/tailscaled"
touch "$directory/$TEST_ROOT/systemd/tailscaled.service" "$directory/$TEST_ROOT/systemd/tailscaled.defaults"
touch "$directory/$TEST_ROOT/systemd/tailscale-online.target" "$directory/$TEST_ROOT/systemd/tailscale-wait-online.service"
`)
			writeCloudInitFakeCommand(t, binDir, "install", "printf '%s\\n' \"$*\" >> \"$INSTALL_LOG\"\n")
			writeCloudInitFakeCommand(t, binDir, "systemctl", "printf '%s\\n' \"$*\" >> \"$SYSTEMCTL_LOG\"\n")
			installLog := filepath.Join(dir, "install.log")
			systemctlLog := filepath.Join(dir, "systemctl.log")
			defaultsPath := filepath.Join(dir, "tailscaled.defaults")
			if err := os.WriteFile(defaultsPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			checksumFail := "0"
			if tc.checksumFail {
				checksumFail = "1"
			}
			cmd := exec.Command("sh", "-c", `. "$1"; install_tailscale`, "test", scriptPath)
			cmd.Env = append(os.Environ(),
				"SERVERPRO_TAILSCALE_SOURCE_ONLY=1",
				"SERVERPRO_TAILSCALE_DEFAULTS_PATH="+defaultsPath,
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"TMPDIR="+dir,
				"TEST_MACHINE="+tc.machine,
				"TEST_ROOT=tailscale_"+tailscaleVersion+"_"+tc.archiveArch,
				"EXPECTED_CHECKSUM="+tc.checksum,
				"CHECKSUM_FAIL="+checksumFail,
				"INSTALL_LOG="+installLog,
				"SYSTEMCTL_LOG="+systemctlLog,
			)
			output, err := cmd.CombinedOutput()
			if tc.checksumFail {
				if err == nil {
					t.Fatalf("checksum mismatch accepted: output=%s", output)
				}
				if _, statErr := os.Stat(installLog); !os.IsNotExist(statErr) {
					t.Fatalf("checksum mismatch reached install: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("approved %s artifact rejected: %v output=%s", tc.archiveArch, err, output)
			}
			installData, err := os.ReadFile(installLog)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"/usr/bin/tailscale", "/usr/sbin/tailscaled", "/etc/systemd/system/tailscaled.service"} {
				if !strings.Contains(string(installData), want) {
					t.Fatalf("approved %s path missing %q: %s", tc.archiveArch, want, installData)
				}
			}
			serviceData, err := os.ReadFile(systemctlLog)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(serviceData), "enable --now tailscaled") {
				t.Fatalf("approved %s path did not start tailscaled: %s", tc.archiveArch, serviceData)
			}
		})
	}
}

func extractTailscaleInstaller(t *testing.T, out string) string {
	t.Helper()
	var document struct {
		RunCmd []any `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(out), &document); err != nil {
		t.Fatalf("decode rendered cloud-init: %v", err)
	}
	for _, command := range document.RunCmd {
		script, ok := command.(string)
		if ok && strings.Contains(script, "TAILSCALE_VERSION=") {
			return script
		}
	}
	t.Fatalf("rendered cloud-init missing Tailscale installer\n%s", out)
	return ""
}

func writeCloudInitFakeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestRenderRequiresAdminUsername(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Admin.Username = ""
	_, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err == nil || !strings.Contains(err.Error(), "admin username") {
		t.Fatalf("expected admin username error, got %v", err)
	}
}

func TestRenderRequiresAuthKey(t *testing.T) {
	cfg := config.Example("prod")
	_, err := Render(Input{Config: cfg, AdminPasswordHash: testAdminPasswordHash})
	if err == nil || !strings.Contains(err.Error(), "tailscale auth key") {
		t.Fatalf("expected auth key error, got %v", err)
	}
}

func TestRenderRequiresAdminPasswordHash(t *testing.T) {
	cfg := config.Example("prod")
	_, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff"})
	if err == nil || !strings.Contains(err.Error(), "admin password hash") {
		t.Fatalf("expected admin password hash error, got %v", err)
	}
}

func TestTemplateHelpersEscapeShellAndJSON(t *testing.T) {
	if got := shell.Quote("prod's web"); got != `'prod'\''s web'` {
		t.Fatalf("shellQuote = %s", got)
	}
	if got := jsonString(`prod"web`); got != `"prod\"web"` {
		t.Fatalf("jsonString = %s", got)
	}
}
