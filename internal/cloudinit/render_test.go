package cloudinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/hostplatform"
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

func TestRenderRegistersSecretCleanupBeforeFallibleCommands(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	capture := strings.Index(out, "TAILSCALE_AUTH_KEY=$(cat /root/.serverpro-tailscale-authkey)")
	trap := strings.Index(out, "trap cleanup EXIT")
	verification := strings.Index(out, "verify_package_minimums")
	if capture < 0 || trap < 0 || verification < 0 || capture > verification || trap > verification {
		t.Fatalf("secret cleanup must be armed before package verification\n%s", out)
	}
	if !strings.Contains(out, `tailscale up --auth-key "${TAILSCALE_AUTH_KEY}"`) {
		t.Fatalf("Tailscale activation must use the captured one-off key\n%s", out)
	}
	if strings.Contains(out, `tailscale up --auth-key "$(cat /root/.serverpro-tailscale-authkey)"`) {
		t.Fatalf("Tailscale activation rereads a cleanup-sensitive key file\n%s", out)
	}
}

func TestRenderPinsSupportedHostAndPackageBaselines(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export LC_ALL=C",
		"EXPECTED_HOST_OS=ubuntu",
		"EXPECTED_HOST_VERSION=24.04",
		"EXPECTED_HOST_CODENAME=noble",
		"EXPECTED_HOST_ARCHITECTURES='x86_64 aarch64 arm64'",
		"verify_package_candidates",
		"verify_package_minimums",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud-init missing supported-host control %q", want)
		}
	}
	for _, pkg := range hostplatform.BasePackageBaselines() {
		if want := pkg.Name + "|" + pkg.MinimumVersion; !strings.Contains(out, want) {
			t.Fatalf("cloud-init missing package baseline %q", want)
		}
	}
}

func TestRenderPreflightsPackageCandidatesBeforeCloudInitInstall(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	candidateCheck := strings.Index(out, "\n    verify_package_candidates\n")
	packageInstall := strings.Index(out, "DEBIAN_FRONTEND=noninteractive apt-get install")
	if candidateCheck < 0 || packageInstall < 0 || candidateCheck > packageInstall {
		t.Fatalf("candidate floors must be checked before cloud-init package installation\n%s", out)
	}
	for _, forbidden := range []string{"package_update:", "package_upgrade:", "\npackages:"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("independent cloud-init package module bypasses candidate preflight: %q\n%s", forbidden, out)
		}
	}
}

func TestRenderedPackageCandidateFloorExecutableMatrix(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		RunCmd []any `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(out), &document); err != nil {
		t.Fatal(err)
	}
	bootstrapScript, ok := document.RunCmd[0].(string)
	if !ok {
		t.Fatalf("first runcmd is not a shell script: %#v", document.RunCmd[0])
	}
	for _, tc := range []struct {
		name, installedState, installedVersion, candidate string
		wantOK                                            bool
	}{
		{name: "candidate-at-floor", candidate: hostplatform.BasePackageBaselines()[0].MinimumVersion, wantOK: true},
		{name: "config-files-newer-candidate-below-floor", installedState: "config-files", installedVersion: "3.0", candidate: "0.0.0"},
		{name: "candidate-below-floor", candidate: "0.0.0"},
		{name: "missing-candidate", candidate: "(none)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			authKeyPath := filepath.Join(dir, "tailscale-auth-key")
			if err := os.WriteFile(authKeyPath, []byte("tskey-auth-oneoff\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			releasePath := filepath.Join(dir, "os-release")
			if err := os.WriteFile(releasePath, []byte("ID=ubuntu\nVERSION_ID=24.04\nVERSION_CODENAME=noble\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			userDataPath := filepath.Join(dir, "user-data.txt")
			instanceDataPath := filepath.Join(dir, "user-data.txt.i")
			script := strings.ReplaceAll(bootstrapScript, "/root/.serverpro-tailscale-authkey", authKeyPath)
			script = strings.ReplaceAll(script, "/var/lib/cloud/instances/*/user-data.txt.i", instanceDataPath)
			script = strings.ReplaceAll(script, "/var/lib/cloud/instances/*/user-data.txt", userDataPath)
			script = strings.ReplaceAll(script, "/etc/os-release", releasePath)
			scriptPath := filepath.Join(dir, "bootstrap.sh")
			if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			installMarker := filepath.Join(dir, "package-mutated")
			writeCloudInitFakeCommand(t, binDir, "uname", "printf 'x86_64\\n'\n")
			writeCloudInitFakeCommand(t, binDir, "dpkg-query", "if [ -f \"$INSTALL_MARKER\" ]; then state=installed; version=$CANDIDATE_VERSION; elif [ -n \"$INSTALLED_STATE\" ]; then state=$INSTALLED_STATE; version=$INSTALLED_VERSION; else exit 1; fi\ncase \"$*\" in *db:Status-Status*) printf '%s|%s' \"$state\" \"$version\" ;; *) printf '%s' \"$version\" ;; esac\n")
			writeCloudInitFakeCommand(t, binDir, "dpkg", "case \"${2:-}\" in 3.0|\"$CANDIDATE_VERSION\") [ \"${2:-}\" != 0.0.0 ] ;; *) exit 1 ;; esac\n")
			writeCloudInitFakeCommand(t, binDir, "apt-cache", "printf '  Candidate: %s\\n' \"$CANDIDATE_VERSION\"\n")
			writeCloudInitFakeCommand(t, binDir, "apt-get", "if [ \"${1:-}\" = update ]; then exit 0; fi\nprintf ran >\"$INSTALL_MARKER\"\n")
			cmd := exec.Command("sh", scriptPath)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"INSTALLED_STATE="+tc.installedState,
				"INSTALLED_VERSION="+tc.installedVersion,
				"CANDIDATE_VERSION="+tc.candidate,
				"INSTALL_MARKER="+installMarker,
			)
			err := cmd.Run()
			if tc.wantOK && err != nil {
				t.Fatalf("safe package candidates rejected: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("unsafe package candidates accepted")
			}
			_, statErr := os.Stat(installMarker)
			if (statErr == nil) != tc.wantOK {
				t.Fatalf("package mutation ran = %v, want %v", statErr == nil, tc.wantOK)
			}
			if _, statErr := os.Stat(authKeyPath); !os.IsNotExist(statErr) {
				t.Fatalf("one-off auth key was not cleaned up: %v", statErr)
			}
		})
	}
}

func TestRenderPinsAndVerifiesTailscaleArtifacts(t *testing.T) {
	cfg := config.Example("prod")
	out, err := Render(Input{Config: cfg, TailscaleAuthKey: "tskey-auth-oneoff", AdminPasswordHash: testAdminPasswordHash})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"TAILSCALE_VERSION=1.102.3",
		"36ddd9b51be57ffc2990cf76323cfa13643bfbb1b8a969f6183fa164741cdef5",
		"a0fa1b154af8c61f862a2259f559f7396d96c0225f4a863eae2333e1546bbe25",
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
