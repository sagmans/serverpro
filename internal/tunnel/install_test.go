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
		origins                                           string
		wantOK                                            bool
	}{
		{name: "candidate-at-floor", candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL, wantOK: true},
		{name: "installed-at-floor-skips-candidate", installedState: "installed", installedVersion: "3.0", candidate: "0.0.0", origins: "https://mirror.example/ubuntu", wantOK: true},
		{name: "config-files-newer-candidate-below-floor", installedState: "config-files", installedVersion: "3.0", candidate: "0.0.0", origins: cloudflareAPTRepositoryURL},
		{name: "candidate-below-floor", candidate: "0.0.0", origins: cloudflareAPTRepositoryURL},
		{name: "missing-candidate", candidate: "(none)"},
		// The origin gate: the candidate version must come only from Cloudflare.
		{name: "candidate-from-other-origin", candidate: baseline.MinimumVersion, origins: "https://mirror.example/cloudflared"},
		{name: "candidate-also-from-other-origin", candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL + " https://mirror.example/cloudflared"},
		{name: "candidate-only-in-dpkg-status", candidate: baseline.MinimumVersion},
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
			// Mirrors apt-cache policy output: a version line per version, then one
			// "priority URL suite arch Packages" line per repository offering it.
			writeFakeCommand(t, binDir, "apt-cache", `printf 'cloudflared:\n  Installed: (none)\n  Candidate: %s\n  Version table:\n' "$CANDIDATE_VERSION"
printf '     %s 990\n' "$CANDIDATE_VERSION"
for origin in $ORIGINS; do printf '        990 %s noble/main amd64 Packages\n' "$origin"; done
printf '        100 /var/lib/dpkg/status\n'
printf ' *** 0.0.1 500\n        500 https://mirror.example/older noble/main amd64 Packages\n'
`)
			cmd := exec.Command("bash", "-c", `source "$1"; verify_cloudflared_candidate`, "test", scriptPath)
			cmd.Env = append(os.Environ(),
				"SERVERPRO_TUNNEL_SOURCE_ONLY=1",
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"INSTALLED_STATE="+tc.installedState,
				"INSTALLED_VERSION="+tc.installedVersion,
				"CANDIDATE_VERSION="+tc.candidate,
				"ORIGINS="+tc.origins,
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
	install := strings.Index(s, "\n  install_cloudflared_candidate\n")
	if candidateCheck < 0 || install < 0 || candidateCheck > install {
		t.Fatalf("cloudflared candidate floor must be checked before package installation\n%s", s)
	}
}

// TestUpgradeScriptGatesTrustBeforeApt pins the doctor repair's safety order:
// it may only reuse provisioning's pinned key and exact source, so every trust
// gate must run before apt, and it must never fetch a key or write the token.
func TestUpgradeScriptGatesTrustBeforeApt(t *testing.T) {
	s := UpgradeScript()
	if out, err := exec.Command("sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Fatalf("upgrade script syntax: %v\n%s", err, out)
	}
	mainStart := strings.Index(s, "main() {")
	if mainStart < 0 {
		t.Fatalf("upgrade script has no main\n%s", s)
	}
	body := s[mainStart:]
	aptUpdate := strings.Index(body, "apt_get update")
	if aptUpdate < 0 {
		t.Fatalf("upgrade script never refreshes apt\n%s", body)
	}
	for _, gate := range []string{"require_supported_host", "require_cloudflared_service_setup", "verify_trusted_cloudflare_apt_key", "verify_cloudflared_apt_source"} {
		if i := strings.Index(body, gate); i < 0 || i > aptUpdate {
			t.Fatalf("%s must run before apt-get update\n%s", gate, body)
		}
	}
	for _, forbidden := range []string{"install_cloudflare_apt_key", "> " + cloudflaredTokenPath, "> " + cloudflaredUnitPath} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("upgrade script must not contain %q\n%s", forbidden, body)
		}
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

// TestScriptsPinCloudflaredOriginAndWaitForAptLock covers both apt paths:
// provisioning and the doctor repair write the origin pin before apt runs,
// install only the verified candidate version, and wait on the dpkg lock.
func TestScriptsPinCloudflaredOriginAndWaitForAptLock(t *testing.T) {
	for name, s := range map[string]string{"install": InstallScript("cf-tunnel-token"), "upgrade": UpgradeScript()} {
		t.Run(name, func(t *testing.T) {
			body := s[strings.Index(s, "main() {"):]
			pin := strings.Index(body, "write_cloudflared_apt_pin")
			update := strings.Index(body, "apt_get update")
			candidate := strings.Index(body, "verify_cloudflared_candidate")
			install := strings.Index(body, "install_cloudflared_candidate")
			if pin < 0 || update < 0 || candidate < 0 || install < 0 || pin > update || update > candidate || candidate > install {
				t.Fatalf("want pin < apt update < candidate check < install\n%s", body)
			}
			if strings.Contains(body, "apt-get ") {
				t.Fatalf("main must call apt through apt_get so it waits for the dpkg lock\n%s", body)
			}
			for _, want := range []string{
				`apt-get -o DPkg::Lock::Timeout="${APT_LOCK_TIMEOUT}"`,
				"APT_LOCK_TIMEOUT=300",
				`apt_get install -y "${CLOUDFLARED_PACKAGE}=${candidate_version}"`,
				"CLOUDFLARED_APT_PREFERENCES_PATH=/etc/apt/preferences.d/serverpro-cloudflared",
				"CLOUDFLARED_APT_REPOSITORY_URL=" + cloudflareAPTRepositoryURL,
			} {
				if !strings.Contains(s, want) {
					t.Fatalf("script missing %q", want)
				}
			}
		})
	}
}

func TestWriteCloudflaredAptPinWritesOriginPreference(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "upgrade.sh")
	if err := os.WriteFile(scriptPath, []byte(UpgradeScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs := filepath.Join(dir, "preferences.d", "serverpro-cloudflared")
	cmd := exec.Command("bash", "-c", `source "$1"; CLOUDFLARED_APT_PREFERENCES_PATH="$2"; write_cloudflared_apt_pin; write_cloudflared_apt_pin`, "test", scriptPath, prefs)
	cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write pin: %v\n%s", err, out)
	}
	got, err := os.ReadFile(prefs)
	if err != nil {
		t.Fatal(err)
	}
	want := "Package: cloudflared\nPin: origin \"pkg.cloudflare.com\"\nPin-Priority: 990\n"
	if string(got) != want {
		t.Fatalf("preferences = %q, want %q", got, want)
	}
	info, err := os.Stat(prefs)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("preferences mode = %v, %v", info.Mode(), err)
	}
	if _, err := os.Stat(prefs + ".serverpro-new"); !os.IsNotExist(err) {
		t.Fatalf("temp preferences file left behind: %v", err)
	}
}
