package tunnel

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

const (
	// testMirrorURL stands in for any configured repository other than
	// Cloudflare's; the origin gate must never trust it.
	testMirrorURL = "https://mirror.example/cloudflared"
	// testBelowAnyFloor is a version every reviewed floor exceeds.
	testBelowAnyFloor = "0.0.0"
	// testDpkgUnpackedState is a dpkg state that is not "installed", so the
	// candidate gate runs even though apt-cache marks the version as current.
	testDpkgUnpackedState = "unpacked"
)

// fakeDpkgCompareVersions really orders versions with sort -V (macOS and
// GNU), because a fake that ignores the minimum would never prove the floor.
// Only "ge" is supported so a new comparison in the script fails loudly.
const fakeDpkgCompareVersions = `[ "${1:-}" = --compare-versions ] && [ "${3:-}" = ge ] || { echo "unsupported fake dpkg call: $*" >&2; exit 2; }
lowest=$(printf '%s\n%s\n' "$2" "$4" | sort -V | head -n 1)
[ "${lowest}" = "$4" ]
`

// fakeAptCachePolicy mirrors apt 2.8 apt-cache policy output on noble with the
// origin pin in place: the version line carries the pin priority (990), each
// source line its own priority (500), the installed version is marked "***",
// and the dpkg status pseudo-source follows the real sources at 100.
const fakeAptCachePolicy = `installed='(none)'
[ "${INSTALLED_STATE}" = installed ] && installed=${INSTALLED_VERSION}
printf 'cloudflared:\n  Installed: %s\n  Candidate: %s\n  Version table:\n' "${installed}" "${CANDIDATE_VERSION}"
if [ -n "${CANDIDATE_MARKED}" ]; then printf ' *** %s 990\n' "${CANDIDATE_VERSION}"; else printf '     %s 990\n' "${CANDIDATE_VERSION}"; fi
for origin in ${ORIGINS}; do printf '        500 %s noble/main arm64 Packages\n' "${origin}"; done
[ -z "${CANDIDATE_STATUS}" ] || printf '        100 /var/lib/dpkg/status\n'
if [ "${INSTALLED_STATE}" = installed ] && [ "${INSTALLED_VERSION}" != "${CANDIDATE_VERSION}" ]; then
  printf ' *** %s 100\n        100 /var/lib/dpkg/status\n' "${INSTALLED_VERSION}"
fi
printf '     0.0.1 500\n        500 https://mirror.example/older noble/main arm64 Packages\n'
`

// versionOneStepBelow derives the closest version under a reviewed floor, so
// the floor case keeps testing the boundary when the baseline moves.
func versionOneStepBelow(t *testing.T, version string) string {
	t.Helper()
	cut := strings.LastIndex(version, ".")
	last, err := strconv.Atoi(version[cut+1:])
	if cut < 0 || err != nil || last == 0 {
		t.Fatalf("cannot derive a version one step below %q", version)
	}
	return version[:cut+1] + strconv.Itoa(last-1)
}

func TestInstallScriptCloudflaredCandidateExecutableMatrix(t *testing.T) {
	baseline := hostplatform.CloudflaredPackageBaseline()
	belowFloor := versionOneStepBelow(t, baseline.MinimumVersion)
	for _, tc := range []struct {
		name, installedState, installedVersion, candidate string
		origins                                           string
		candidateMarked, candidateStatus                  bool
		wantOK                                            bool
	}{
		{name: "candidate-at-floor", candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL, wantOK: true},
		{name: "installed-at-floor-skips-candidate", installedState: "installed", installedVersion: baseline.MinimumVersion, candidate: testBelowAnyFloor, origins: testMirrorURL, wantOK: true},
		{name: "installed-below-floor-upgrades-from-cloudflare", installedState: "installed", installedVersion: belowFloor, candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL, wantOK: true},
		{name: "installed-below-floor-candidate-below-floor", installedState: "installed", installedVersion: belowFloor, candidate: belowFloor, origins: cloudflareAPTRepositoryURL, candidateMarked: true, candidateStatus: true},
		{name: "config-files-newer-candidate-below-floor", installedState: "config-files", installedVersion: baseline.MinimumVersion, candidate: testBelowAnyFloor, origins: cloudflareAPTRepositoryURL},
		{name: "candidate-below-floor", candidate: testBelowAnyFloor, origins: cloudflareAPTRepositoryURL},
		{name: "candidate-one-step-below-floor", candidate: belowFloor, origins: cloudflareAPTRepositoryURL},
		{name: "missing-candidate", candidate: "(none)"},
		// The status line has two fields like a version line; it must not end
		// the candidate's source list or hide a source listed after it.
		{name: "marked-candidate-with-origin-and-status", installedState: testDpkgUnpackedState, installedVersion: baseline.MinimumVersion, candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL, candidateMarked: true, candidateStatus: true, wantOK: true},
		{name: "candidate-with-origin-and-status", installedState: testDpkgUnpackedState, installedVersion: baseline.MinimumVersion, candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL, candidateStatus: true, wantOK: true},
		{name: "marked-candidate-also-from-other-origin", installedState: testDpkgUnpackedState, installedVersion: baseline.MinimumVersion, candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL + " " + testMirrorURL, candidateMarked: true, candidateStatus: true},
		// The origin gate: the candidate version must come only from Cloudflare.
		{name: "candidate-from-other-origin", candidate: baseline.MinimumVersion, origins: testMirrorURL},
		{name: "candidate-also-from-other-origin", candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL + " " + testMirrorURL},
		{name: "candidate-only-in-dpkg-status", installedState: testDpkgUnpackedState, installedVersion: baseline.MinimumVersion, candidate: baseline.MinimumVersion, candidateMarked: true, candidateStatus: true},
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
			writeFakeCommand(t, binDir, "dpkg", fakeDpkgCompareVersions)
			writeFakeCommand(t, binDir, "apt-cache", fakeAptCachePolicy)
			cmd := exec.Command("bash", "-c", `source "$1"; verify_cloudflared_candidate`, "test", scriptPath)
			cmd.Env = append(os.Environ(),
				"SERVERPRO_TUNNEL_SOURCE_ONLY=1",
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"INSTALLED_STATE="+tc.installedState,
				"INSTALLED_VERSION="+tc.installedVersion,
				"CANDIDATE_VERSION="+tc.candidate,
				"ORIGINS="+tc.origins,
				"CANDIDATE_MARKED="+flagEnv(tc.candidateMarked),
				"CANDIDATE_STATUS="+flagEnv(tc.candidateStatus),
			)
			out, err := cmd.CombinedOutput()
			if tc.wantOK && err != nil {
				t.Fatalf("safe cloudflared candidate rejected: %v\n%s", err, out)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("unsafe cloudflared candidate accepted")
			}
		})
	}
}

// flagEnv renders a case flag for the fake commands, which test for a
// non-empty value.
func flagEnv(set bool) string {
	if set {
		return "1"
	}
	return ""
}

// TestCloudflaredCandidateOriginParsesRealPolicyOutput feeds the origin gate
// apt-cache policy output captured from apt 2.8.3 on noble arm64 with the pin
// written, before and after installing cloudflared, plus variants that must
// fail closed.
func TestCloudflaredCandidateOriginParsesRealPolicyOutput(t *testing.T) {
	const (
		header      = "cloudflared:\n  Installed: (none)\n  Candidate: 2026.10.0\n  Version table:\n"
		cloudflare  = "        500 https://pkg.cloudflare.com/cloudflared noble/main arm64 Packages\n"
		mirror      = "        500 file:/srv/mirror ./ Packages\n"
		statusLine  = "        100 /var/lib/dpkg/status\n"
		candidate   = "     2026.10.0 990\n"
		installed   = " *** 2026.10.0 990\n"
		wantVersion = "2026.10.0"
	)
	for _, tc := range []struct {
		name, policy string
		wantOK       bool
	}{
		{name: "before-install", policy: header + candidate + cloudflare, wantOK: true},
		{name: "after-install", policy: header + installed + cloudflare + statusLine, wantOK: true},
		{name: "second-repository", policy: header + candidate + cloudflare + mirror},
		{name: "second-repository-after-status-line", policy: header + installed + cloudflare + statusLine + mirror},
		{name: "only-other-repository", policy: header + candidate + mirror},
		{name: "only-status-line", policy: header + installed + statusLine},
		{name: "unrecognised-source-line", policy: header + candidate + cloudflare + "        unexpected\n"},
		{name: "candidate-missing-from-table", policy: header + "     2026.9.0 990\n" + cloudflare},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			scriptPath := filepath.Join(dir, "upgrade.sh")
			if err := os.WriteFile(scriptPath, []byte(UpgradeScript()), 0o600); err != nil {
				t.Fatal(err)
			}
			policyPath := filepath.Join(dir, "policy.txt")
			if err := os.WriteFile(policyPath, []byte(tc.policy), 0o600); err != nil {
				t.Fatal(err)
			}
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			writeFakeCommand(t, binDir, "apt-cache", "cat \"$POLICY\"\n")
			cmd := exec.Command("bash", "-c", `source "$1"; cloudflared_candidate_from_trusted_origin "$2"`, "test", scriptPath, wantVersion)
			cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"), "POLICY="+policyPath)
			err := cmd.Run()
			if tc.wantOK && err != nil {
				t.Fatalf("Cloudflare-only candidate rejected: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("candidate with an untrusted or unknown source accepted")
			}
		})
	}
}

// TestCloudflareAPTOriginMatchesRepositoryHost keeps the pinned origin on the
// repository host apt reports as the origin, so a URL change cannot leave the
// pin naming a host no source uses.
func TestCloudflareAPTOriginMatchesRepositoryHost(t *testing.T) {
	repository, err := url.Parse(cloudflareAPTRepositoryURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := cloudflareAPTOrigin(); got != repository.Hostname() || got != "pkg.cloudflare.com" {
		t.Fatalf("cloudflareAPTOrigin() = %q, want host of %q", got, cloudflareAPTRepositoryURL)
	}
	if !strings.Contains(cloudflaredAPTPreferences(), "Pin: origin \""+repository.Hostname()+"\"\n") {
		t.Fatalf("preferences do not pin the repository host:\n%s", cloudflaredAPTPreferences())
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
	aptUpdate := strings.Index(body, "apt_update")
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
// install only the verified candidate version, wait on the dpkg lock, and
// refresh lists through the host-wide lists-lock wait.
func TestScriptsPinCloudflaredOriginAndWaitForAptLock(t *testing.T) {
	for name, s := range map[string]string{"install": InstallScript("cf-tunnel-token"), "upgrade": UpgradeScript()} {
		t.Run(name, func(t *testing.T) {
			body := s[strings.Index(s, "main() {"):]
			pin := strings.Index(body, "write_cloudflared_apt_pin")
			update := strings.Index(body, "apt_update")
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
				"APT_LOCK_TIMEOUT=" + strconv.Itoa(hostplatform.APTLockTimeoutSeconds),
				hostplatform.APTUpdateShellFunction(),
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
	if _, err := os.Stat(prefs + cloudflaredAPTPinStagingSuffix); !os.IsNotExist(err) {
		t.Fatalf("temp preferences file left behind: %v", err)
	}
}

// TestUpgradeScriptAPTUpdateWaitsForListsLock runs the rendered helper under
// dash, as the sudo transport does, so a held lists lock delays the repair's
// refresh instead of failing it.
func TestUpgradeScriptAPTUpdateWaitsForListsLock(t *testing.T) {
	if _, err := exec.LookPath("dash"); err != nil {
		t.Skip("dash is required to mirror supported Ubuntu/Debian /bin/sh")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "upgrade.sh")
	if err := os.WriteFile(scriptPath, []byte(UpgradeScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	callsPath := filepath.Join(dir, "calls")
	writeFakeCommand(t, binDir, "apt-get", `count=$(cat "$APT_CALLS" 2>/dev/null || printf 0)
count=$((count + 1))
printf '%s' "$count" > "$APT_CALLS"
if [ "$count" -eq 1 ]; then
  printf 'E: %s. It is held by process 42 (apt-get)\n' "$LOCK_ERROR" >&2
  exit 100
fi
`)
	writeFakeCommand(t, binDir, "sleep", "exit 0\n")
	cmd := exec.Command("dash", "-c", `. "$1"; apt_update`, "test", scriptPath)
	cmd.Env = append(os.Environ(),
		"SERVERPRO_TUNNEL_SOURCE_ONLY=1",
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"APT_CALLS="+callsPath,
		"LOCK_ERROR="+hostplatform.APTListsLockError,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apt_update after a released lists lock: %v\n%s", err, out)
	}
	if calls, err := os.ReadFile(callsPath); err != nil || string(calls) != "2" {
		t.Fatalf("apt-get calls = %q, %v; want one retry after the lock error", calls, err)
	}
}
