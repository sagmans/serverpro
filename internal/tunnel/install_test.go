package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/hostplatform"
)

func TestCheckCommandVerifiesPackageFloorAndService(t *testing.T) {
	command := CheckCommand()
	baseline := hostplatform.CloudflaredPackageBaseline()
	for _, want := range []string{baseline.Name, baseline.MinimumVersion, "db:Status-Status", "dpkg --compare-versions", "systemctl is-active"} {
		if !strings.Contains(command, want) {
			t.Fatalf("check command missing %q: %s", want, command)
		}
	}
}

func TestCheckCommandRejectsRemovedPackageState(t *testing.T) {
	binDir := t.TempDir()
	writeFakeCommand(t, binDir, "dpkg-query", "case \"$*\" in *db:Status-Status*) printf 'config-files|3.0' ;; *) printf '3.0' ;; esac\n")
	writeFakeCommand(t, binDir, "dpkg", "exit 0\n")
	writeFakeCommand(t, binDir, "systemctl", "exit 0\n")
	cmd := exec.Command("sh", "-c", CheckCommand())
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	if err := cmd.Run(); err == nil {
		t.Fatal("removed cloudflared package passed the service check")
	}
}

func TestInstallScriptCleansTokenFile(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	for _, want := range []string{"/etc/cloudflared/token", "--token-file /etc/cloudflared/token", "chmod 0600 /etc/cloudflared/token", "systemctl is-active cloudflared"} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q\n%s", want, s)
		}
	}
}

func TestInstallScriptPinsSupportedHostAndCloudflaredMinimum(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	baseline := hostplatform.CloudflaredPackageBaseline()
	for _, want := range []string{
		"export LC_ALL=C",
		"EXPECTED_HOST_OS=" + hostplatform.ManagedHostOS,
		"EXPECTED_HOST_VERSION=" + hostplatform.ManagedHostVersion,
		"EXPECTED_HOST_CODENAME=" + hostplatform.ManagedHostCodename,
		"EXPECTED_HOST_ARCHITECTURES='x86_64 aarch64 arm64'",
		"CLOUDFLARED_PACKAGE=" + baseline.Name,
		"CLOUDFLARED_MINIMUM_VERSION=" + baseline.MinimumVersion,
		"dpkg --compare-versions",
		"cloudflared " + hostplatform.ManagedHostCodename + " main",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q\n%s", want, s)
		}
	}
}

func TestInstallScriptCloudflaredCandidateExecutableMatrix(t *testing.T) {
	baseline := hostplatform.CloudflaredPackageBaseline()
	for _, tc := range []struct {
		name, installedState, installedVersion, candidate string
		wantOK                                            bool
	}{
		{name: "candidate-at-floor", candidate: baseline.MinimumVersion, wantOK: true},
		{name: "config-files-newer-candidate-below-floor", installedState: "config-files", installedVersion: "3.0", candidate: "0.0.0"},
		{name: "candidate-below-floor", candidate: "0.0.0"},
		{name: "missing-candidate", candidate: "(none)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			scriptPath := filepath.Join(dir, "install.sh")
			if err := os.WriteFile(scriptPath, []byte(InstallScript("cf-tunnel-token")), 0o600); err != nil {
				t.Fatal(err)
			}
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			writeFakeCommand(t, binDir, "dpkg-query", "[ -n \"$INSTALLED_STATE\" ] || exit 1\ncase \"$*\" in *db:Status-Status*) printf '%s|%s' \"$INSTALLED_STATE\" \"$INSTALLED_VERSION\" ;; *) printf '%s' \"$INSTALLED_VERSION\" ;; esac\n")
			writeFakeCommand(t, binDir, "dpkg", "case \"${2:-}\" in 3.0) exit 0 ;; \"$CANDIDATE_VERSION\") [ \"${2:-}\" != 0.0.0 ] ;; *) exit 1 ;; esac\n")
			writeFakeCommand(t, binDir, "apt-cache", "printf '  Candidate: %s\\n' \"$CANDIDATE_VERSION\"\n")
			cmd := exec.Command("bash", "-c", `source "$1"; verify_cloudflared_candidate`, "test", scriptPath)
			cmd.Env = append(os.Environ(),
				"SERVERPRO_TUNNEL_SOURCE_ONLY=1",
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"INSTALLED_STATE="+tc.installedState,
				"INSTALLED_VERSION="+tc.installedVersion,
				"CANDIDATE_VERSION="+tc.candidate,
			)
			err := cmd.Run()
			if tc.wantOK && err != nil {
				t.Fatalf("safe cloudflared candidate rejected: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("unsafe cloudflared candidate accepted")
			}
		})
	}
}

func TestInstallScriptPreflightsCloudflaredCandidateBeforeInstall(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	candidateCheck := strings.Index(s, "\n  verify_cloudflared_candidate\n")
	install := strings.Index(s, `apt-get install -y "${CLOUDFLARED_PACKAGE}"`)
	if candidateCheck < 0 || install < 0 || candidateCheck > install {
		t.Fatalf("cloudflared candidate floor must be checked before package installation\n%s", s)
	}
}

func TestInstallScriptRejectsUnsupportedHostBeforeMutation(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	hostGate := strings.Index(s, "main() {\n  require_supported_host")
	mutation := strings.Index(s, "install -d -m 0755 /usr/share/keyrings")
	if hostGate < 0 || mutation < 0 || hostGate > mutation {
		t.Fatalf("supported-host gate must precede managed mutation\n%s", s)
	}
}

func TestInstallScriptVerifiesCloudflareAptKeyBeforeTrust(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	for _, want := range []string{cloudflareAPTKeyFingerprint, "mktemp", "gpg --show-keys --with-colons", "install -m 0644", "cloudflare_key_publish_tmp", "mv -f", "signed-by=/usr/share/keyrings/cloudflare-main.gpg", "chmod 0644 /etc/apt/sources.list.d/cloudflared.list"} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "-o /usr/share/keyrings/cloudflare-main.gpg") {
		t.Fatalf("script must not download directly into the trusted keyring\n%s", s)
	}
}

func TestInstallScriptRejectsMismatchedCloudflareAptKey(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	// Refuse to source older script shapes because their top-level privileged
	// commands would run during this executable regression test.
	if !strings.Contains(s, "SERVERPRO_TUNNEL_SOURCE_ONLY") {
		t.Fatalf("script needs source-only guard before executable helper testing\n%s", s)
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakeCommand(t, binDir, "curl", "for arg; do output=$arg; done\nprintf 'untrusted' > \"$output\"\n")
	writeFakeCommand(t, binDir, "gpg", "printf 'fpr:::::::::0000000000000000000000000000000000000000:\\n'\n")
	installMarker := filepath.Join(dir, "install-ran")
	writeFakeCommand(t, binDir, "install", "printf ran > \"$INSTALL_MARKER\"\n")

	cmd := exec.Command("bash", "-c", `source "$1"; install_cloudflare_apt_key "$2"`, "test", scriptPath, filepath.Join(dir, "trusted.gpg"))
	cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"), "INSTALL_MARKER="+installMarker)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "fingerprint mismatch") {
		t.Fatalf("mismatched key should fail closed: err=%v output=%s", err, out)
	}
	if _, err := os.Stat(installMarker); !os.IsNotExist(err) {
		t.Fatalf("mismatched key reached trusted install: %v", err)
	}
}

func TestInstallScriptAcceptsApprovedCloudflarePrimaryKeyWithSubkey(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakeCommand(t, binDir, "curl", "for arg; do output=$arg; done\nprintf 'approved' > \"$output\"\n")
	writeFakeCommand(t, binDir, "gpg", "printf 'pub:::::::::\\nfpr:::::::::"+cloudflareAPTKeyFingerprint+":\\nsub:::::::::\\nfpr:::::::::0000000000000000000000000000000000000000:\\n'\n")
	destination := filepath.Join(dir, "trusted.gpg")
	cmd := exec.Command("bash", "-c", `source "$1"; install_cloudflare_apt_key "$2"`, "test", scriptPath, destination)
	cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("approved primary key rejected: %v output=%s", err, out)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("approved key was not published: %v", err)
	}
}

func TestInstallScriptRejectsAdditionalCloudflarePrimaryKey(t *testing.T) {
	s := InstallScript("cf-tunnel-token")
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakeCommand(t, binDir, "curl", "for arg; do output=$arg; done\nprintf 'untrusted' > \"$output\"\n")
	writeFakeCommand(t, binDir, "gpg", "printf 'pub:::::::::\\nfpr:::::::::"+cloudflareAPTKeyFingerprint+":\\npub:::::::::\\nfpr:::::::::0000000000000000000000000000000000000000:\\n'\n")
	installMarker := filepath.Join(dir, "install-ran")
	writeFakeCommand(t, binDir, "install", "printf ran > \"$INSTALL_MARKER\"\n")

	cmd := exec.Command("bash", "-c", `source "$1"; install_cloudflare_apt_key "$2"`, "test", scriptPath, filepath.Join(dir, "trusted.gpg"))
	cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"), "INSTALL_MARKER="+installMarker)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "fingerprint mismatch") {
		t.Fatalf("additional primary key should fail closed: err=%v output=%s", err, out)
	}
	if _, err := os.Stat(installMarker); !os.IsNotExist(err) {
		t.Fatalf("additional primary key reached trusted install: %v", err)
	}
}

func writeFakeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}
