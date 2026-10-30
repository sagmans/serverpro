package tunnel

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/hostplatform"
)

// TestCheckCommandVerifiesPackageFloorAndService covers the managed check and
// the ingress-"none" audit, which must render the same floor, the production
// binary and receipt paths, and the service gate.
func TestCheckCommandVerifiesPackageFloorAndService(t *testing.T) {
	baseline := hostplatform.CloudflaredPackageBaseline()
	for name, command := range map[string]string{"managed": CheckCommand(), "unmanaged": UnmanagedCheckCommand()} {
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{baseline.Name, baseline.MinimumVersion, "db:Status-Status", "dpkg --compare-versions", "systemctl is-active", cloudflaredBinaryPath, cloudflaredReceiptPath} {
				if !strings.Contains(command, want) {
					t.Fatalf("check command missing %q: %s", want, command)
				}
			}
			if out, err := exec.Command("sh", "-n", "-c", command).CombinedOutput(); err != nil {
				t.Fatalf("check command syntax: %v\n%s", err, out)
			}
		})
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
	// testBinaryDigest is what the fake sha256sum reports for the connector,
	// so a case opts into a matching install receipt by recording it.
	testBinaryDigest = "0123456789abcdef"
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
		receipt                                           bool
		wantOK                                            bool
	}{
		{name: "candidate-at-floor", candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL, wantOK: true},
		{name: "installed-at-floor-with-receipt-skips-candidate", installedState: "installed", installedVersion: baseline.MinimumVersion, candidate: testBelowAnyFloor, origins: testMirrorURL, receipt: true, wantOK: true},
		// Without a matching receipt the floor alone no longer keeps a
		// connector: it must be replaced by a Cloudflare-only version.
		{name: "installed-at-floor-without-receipt-needs-cloudflare-version", installedState: "installed", installedVersion: baseline.MinimumVersion, candidate: testBelowAnyFloor, origins: testMirrorURL},
		{name: "installed-newer-without-receipt-replaced-from-cloudflare", installedState: "installed", installedVersion: "2099.1.0", candidate: baseline.MinimumVersion, origins: cloudflareAPTRepositoryURL, wantOK: true},
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
			writeFakeCommand(t, binDir, "sha256sum", "printf '%s  %s\\n' \""+testBinaryDigest+"\" \"$1\"\n")
			receiptPath := filepath.Join(dir, "cloudflared.receipt")
			if tc.receipt {
				if err := os.WriteFile(receiptPath, []byte(tc.installedVersion+" "+testBinaryDigest+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-c", `source "$1"; CLOUDFLARED_RECEIPT="$2"; verify_cloudflared_candidate`, "test", scriptPath, receiptPath)
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
			cmd := exec.Command("bash", "-c", `source "$1"; cloudflared_versions_from_trusted_origin`, "test", scriptPath)
			cmd.Env = append(os.Environ(), "SERVERPRO_TUNNEL_SOURCE_ONLY=1", "PATH="+binDir+":"+os.Getenv("PATH"), "POLICY="+policyPath)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("origin parser failed: %v", err)
			}
			trusted := slices.Contains(strings.Fields(string(out)), wantVersion)
			if tc.wantOK && !trusted {
				t.Fatalf("Cloudflare-only candidate rejected: %q", out)
			}
			if !tc.wantOK && trusted {
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
				`apt_get install -y "$@" "${CLOUDFLARED_PACKAGE}=${candidate_version}"`,
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
	if _, err := os.Stat(prefs + stagingSuffix); !os.IsNotExist(err) {
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

const (
	// testReinstalledDigest is the digest the fake apt-get gives the binary it
	// installs, so a repaired receipt is told apart from the original build.
	testReinstalledDigest = "fedcba9876543210"
	// testForeignDigest names a receipt recorded for some other build.
	testForeignDigest = "feedface00000000"
	// testPgrepNoMatch is pgrep's exit status when no process matches.
	testPgrepNoMatch = "1"
	// testDpkgQueryUnknownPackage is dpkg-query's status for a package dpkg
	// has no record of; any other failure status is a query error.
	testDpkgQueryUnknownPackage = "1"
)

// Receipt shapes an unmanaged-host case can start from.
const (
	receiptNone = iota
	receiptMatching
	receiptForeign
)

// unmanagedHost is the live state an unmanaged-host case starts from. The
// fakes read and write it through files, so a repair's apt and systemctl
// calls are visible to the recheck that follows it.
type unmanagedHost struct {
	dpkgState, version         string
	packagedBinary, stray      bool
	receipt                    int
	load, unitFile, active     string
	pgrepStatus, queryStatus   string
	unit, token, changedSource bool
	enableOnInstall            bool
	// legacySource writes the line older provisioning used, which repair
	// must migrate rather than refuse.
	legacySource bool
}

// unmanagedHarness is a scratch host for the unmanaged check and repair:
// fake dpkg, apt, systemd, and process tools on a PATH that holds only the
// system directories, so a developer's own cloudflared cannot leak in.
type unmanagedHarness struct {
	dir, stateDir, binDir, usrBin, binaryPath, receiptPath string
	env                                                    []string
}

// dormantHost is a present package whose service was never enabled or
// started, the shape a leftover connector install leaves behind.
func dormantHost(version string, receipt int) unmanagedHost {
	return unmanagedHost{dpkgState: "installed", version: version, packagedBinary: true, receipt: receipt, load: "loaded", unitFile: "disabled", active: "inactive"}
}

// activeHost is a present package whose service runs and has the unit and
// token the managed repair requires.
func activeHost(version string, receipt int) unmanagedHost {
	return unmanagedHost{dpkgState: "installed", version: version, packagedBinary: true, receipt: receipt, load: "loaded", unitFile: "enabled", active: "active", unit: true, token: true}
}

func newUnmanagedHarness(t *testing.T, host unmanagedHost) unmanagedHarness {
	t.Helper()
	for _, systemPath := range []string{"/usr/bin/cloudflared", "/bin/cloudflared"} {
		if _, err := os.Stat(systemPath); err == nil {
			t.Skipf("%s exists on this machine and would shadow the scratch host", systemPath)
		}
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := unmanagedHarness{
		dir:         dir,
		stateDir:    filepath.Join(dir, "state"),
		binDir:      filepath.Join(dir, "bin"),
		usrBin:      filepath.Join(dir, "usr-bin"),
		receiptPath: filepath.Join(dir, "cloudflared.receipt"),
	}
	h.binaryPath = filepath.Join(h.usrBin, "cloudflared")
	strayBin := filepath.Join(dir, "stray-bin")
	for _, d := range []string{h.stateDir, h.binDir, h.usrBin, strayBin} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"state": host.dpkgState, "version": host.version, "digest": testBinaryDigest,
		"load": host.load, "unit_file": host.unitFile, "active": host.active,
	}
	for name, content := range files {
		writeTestFile(t, filepath.Join(h.stateDir, name), content)
	}
	switch host.receipt {
	case receiptMatching:
		writeTestFile(t, h.receiptPath, host.version+" "+testBinaryDigest+"\n")
	case receiptForeign:
		writeTestFile(t, h.receiptPath, host.version+" "+testForeignDigest+"\n")
	}
	if host.packagedBinary {
		writeFakeCommand(t, h.usrBin, "cloudflared", "exit 0\n")
	}
	if host.stray {
		writeFakeCommand(t, strayBin, "cloudflared", "exit 0\n")
	}
	if host.unit {
		writeTestFile(t, filepath.Join(dir, "cloudflared.service"), "[Unit]\n")
	}
	if host.token {
		writeTestFile(t, filepath.Join(dir, "token"), "scratch-token")
	}
	source := cloudflaredAPTSource()
	if host.changedSource {
		source = "deb " + testMirrorURL + " noble main"
	}
	if host.legacySource {
		source = cloudflaredLegacyAPTSource()
	}
	writeTestFile(t, filepath.Join(dir, "cloudflared.list"), source+"\n")
	writeTestFile(t, filepath.Join(dir, "cloudflare-main.gpg"), "scratch keyring")

	writeFakeCommand(t, h.binDir, "dpkg-query", `state=$(cat "$STATE_DIR/state")
[ -n "$state" ] || exit "$DPKG_QUERY_MISSING_STATUS"
case "$*" in
  *'|${Version}'*) printf '%s|%s' "$state" "$(cat "$STATE_DIR/version")" ;;
  *) printf '%s' "$state" ;;
esac
`)
	writeFakeCommand(t, h.binDir, "dpkg", fakeDpkgCompareVersions)
	writeFakeCommand(t, h.binDir, "apt-cache", fakeAptCachePolicy)
	writeFakeCommand(t, h.binDir, "sha256sum", `printf '%s  %s\n' "$(cat "$STATE_DIR/digest")" "$1"`+"\n")
	writeFakeCommand(t, h.binDir, "pgrep", `exit "$PGREP_STATUS"`+"\n")
	writeFakeCommand(t, h.binDir, "gpg", "printf 'pub:::::::::\\nfpr:::::::::"+cloudflareAPTKeyFingerprint+":\\n'\n")
	writeFakeCommand(t, h.binDir, "systemctl", `case "$1" in
  show)
    case "$2" in
      --property=LoadState) cat "$STATE_DIR/load" ;;
      --property=UnitFileState) cat "$STATE_DIR/unit_file" ;;
      --property=ActiveState) cat "$STATE_DIR/active" ;;
      *) echo "unsupported fake systemctl call: $*" >&2; exit 2 ;;
    esac
    ;;
  is-active)
    state=$(cat "$STATE_DIR/active")
    printf '%s\n' "$state"
    [ "$state" = active ]
    ;;
  enable|restart)
    printf '%s\n' "$*" >> "$STATE_DIR/systemctl.log"
    if [ "$1" = enable ]; then printf enabled > "$STATE_DIR/unit_file"; else printf active > "$STATE_DIR/active"; fi
    ;;
  *) echo "unsupported fake systemctl call: $*" >&2; exit 2 ;;
esac
`)
	writeFakeCommand(t, h.binDir, "apt-get", `printf '%s\n' "$*" >> "$STATE_DIR/apt.log"
for arg; do last=$arg; done
case "$last" in
  update) ;;
  cloudflared=*)
    printf installed > "$STATE_DIR/state"
    printf '%s' "${last#cloudflared=}" > "$STATE_DIR/version"
    printf '%s' "$REINSTALLED_DIGEST" > "$STATE_DIR/digest"
    if [ -n "$ENABLE_ON_INSTALL" ]; then printf enabled > "$STATE_DIR/unit_file"; fi
    ;;
  *) echo "unsupported fake apt-get call: $*" >&2; exit 2 ;;
esac
`)

	pgrepStatus := host.pgrepStatus
	if pgrepStatus == "" {
		pgrepStatus = testPgrepNoMatch
	}
	queryStatus := host.queryStatus
	if queryStatus == "" {
		queryStatus = testDpkgQueryUnknownPackage
	}
	path := h.binDir
	if host.stray {
		path += ":" + strayBin
	}
	baseline := hostplatform.CloudflaredPackageBaseline()
	h.env = append(os.Environ(),
		"PATH="+path+":"+h.usrBin+":/usr/bin:/bin",
		"STATE_DIR="+h.stateDir,
		"PGREP_STATUS="+pgrepStatus,
		"DPKG_QUERY_MISSING_STATUS="+queryStatus,
		"REINSTALLED_DIGEST="+testReinstalledDigest,
		"ENABLE_ON_INSTALL="+flagEnv(host.enableOnInstall),
		"INSTALLED_STATE="+host.dpkgState,
		"INSTALLED_VERSION="+host.version,
		"CANDIDATE_VERSION="+baseline.MinimumVersion,
		"ORIGINS="+cloudflareAPTRepositoryURL,
		"CANDIDATE_MARKED=",
		"CANDIDATE_STATUS=",
	)
	return h
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// posixShell prefers dash because the sudo transport runs scripts with the
// managed host's /bin/sh, which is dash on Ubuntu.
func posixShell() string {
	if _, err := exec.LookPath("dash"); err == nil {
		return "dash"
	}
	return "sh"
}

func (h unmanagedHarness) check(t *testing.T) (string, error) {
	t.Helper()
	cmd := exec.Command(posixShell(), "-c", unmanagedCheckCommand(h.binaryPath, h.receiptPath))
	cmd.Env = h.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// repair runs the rendered repair with its host paths pointed at the scratch
// tree and the supported-host gate stubbed, since the test machine is not a
// managed Ubuntu host.
func (h unmanagedHarness) repair(t *testing.T) (string, error) {
	t.Helper()
	scriptPath := filepath.Join(h.dir, "repair.sh")
	writeTestFile(t, scriptPath, UnmanagedRepairScript())
	cmd := exec.Command(posixShell(), "-c", `. "$1"
CLOUDFLARED_BINARY=$2
CLOUDFLARED_RECEIPT=$3
CLOUDFLARED_UNIT=$4/cloudflared.service
CLOUDFLARED_TOKEN=$4/token
CLOUDFLARE_KEYRING=$4/cloudflare-main.gpg
CLOUDFLARED_APT_SOURCE_PATH=$4/cloudflared.list
CLOUDFLARED_APT_PREFERENCES_PATH=$4/preferences.d/serverpro-cloudflared
require_supported_host() { :; }
main`, "test", scriptPath, h.binaryPath, h.receiptPath, h.dir)
	cmd.Env = append(h.env, "SERVERPRO_TUNNEL_SOURCE_ONLY=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// stateLog returns a fake's call log, empty when the fake was never called.
func (h unmanagedHarness) stateLog(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.stateDir, name))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// TestUnmanagedCheckCommandExecutableMatrix runs the rendered ingress-"none"
// audit against absent, active, and dormant hosts and the states it must
// refuse to classify, so an unexpected host never reads as absent.
func TestUnmanagedCheckCommandExecutableMatrix(t *testing.T) {
	baseline := hostplatform.CloudflaredPackageBaseline()
	belowFloor := versionOneStepBelow(t, baseline.MinimumVersion)
	activeStopped := activeHost(baseline.MinimumVersion, receiptMatching)
	activeStopped.active = "failed"
	dormantNoUnit := dormantHost(baseline.MinimumVersion, receiptMatching)
	dormantNoUnit.load, dormantNoUnit.unitFile = "not-found", ""
	runningOutside := dormantHost(baseline.MinimumVersion, receiptMatching)
	runningOutside.pgrepStatus = "0"
	staticUnit := dormantHost(baseline.MinimumVersion, receiptMatching)
	staticUnit.unitFile = "static"
	halfInstalled := dormantHost(baseline.MinimumVersion, receiptMatching)
	halfInstalled.dpkgState = "half-installed"
	strayBinary := dormantHost(baseline.MinimumVersion, receiptMatching)
	strayBinary.stray = true
	for _, tc := range []struct {
		name    string
		host    unmanagedHost
		wantOK  bool
		wantOut string
	}{
		{name: "absent", host: unmanagedHost{}, wantOK: true, wantOut: "cloudflared not installed"},
		{name: "absent-config-files", host: unmanagedHost{dpkgState: "config-files", version: belowFloor}, wantOK: true, wantOut: "cloudflared not installed"},
		{name: "binary-without-package", host: unmanagedHost{packagedBinary: true, load: "not-found", active: "inactive"}, wantOut: "cloudflared package is not installed"},
		{name: "stray-binary-on-path", host: strayBinary, wantOut: "cloudflared on PATH is"},
		{name: "active-at-floor", host: activeHost(baseline.MinimumVersion, receiptMatching), wantOK: true, wantOut: "cloudflared " + baseline.MinimumVersion + " active"},
		{name: "active-below-floor", host: activeHost(belowFloor, receiptMatching), wantOut: "is below minimum"},
		{name: "active-without-receipt", host: activeHost(baseline.MinimumVersion, receiptNone), wantOut: "install receipt is missing"},
		{name: "enabled-but-stopped", host: activeStopped, wantOut: "cloudflared service is not active: failed"},
		{name: "dormant-at-floor", host: dormantHost(baseline.MinimumVersion, receiptMatching), wantOK: true, wantOut: "cloudflared " + baseline.MinimumVersion + " dormant"},
		{name: "dormant-without-unit", host: dormantNoUnit, wantOK: true, wantOut: "dormant"},
		{name: "dormant-below-floor", host: dormantHost(belowFloor, receiptNone), wantOut: "is below minimum"},
		{name: "dormant-foreign-receipt", host: dormantHost(baseline.MinimumVersion, receiptForeign), wantOut: "is not the build serverpro installed"},
		{name: "half-installed", host: halfInstalled, wantOut: "package status is half-installed"},
		{name: "half-installed-without-binary", host: unmanagedHost{dpkgState: "half-installed", version: belowFloor}, wantOut: "package status is half-installed"},
		{name: "process-outside-service", host: runningOutside, wantOut: "running outside cloudflared.service"},
		{name: "unclassified-unit-state", host: staticUnit, wantOut: "unit state is loaded/static"},
		{name: "dpkg-query-error", host: unmanagedHost{queryStatus: "2"}, wantOut: "dpkg-query cloudflared failed with status 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := newUnmanagedHarness(t, tc.host).check(t)
			if tc.wantOK != (err == nil) {
				t.Fatalf("check err = %v, wantOK %v\n%s", err, tc.wantOK, out)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("check output = %q, want %q", out, tc.wantOut)
			}
		})
	}
}

// TestUnmanagedRepairScriptExecutableMatrix runs the rendered ingress-"none"
// repair and then the audit again. An active connector gets the managed
// repair and restart; a dormant one is reinstalled from the trusted origin
// without a token and is never enabled or started; anything the repair
// cannot trust stops before apt.
func TestUnmanagedRepairScriptExecutableMatrix(t *testing.T) {
	baseline := hostplatform.CloudflaredPackageBaseline()
	belowFloor := versionOneStepBelow(t, baseline.MinimumVersion)
	activeNoToken := activeHost(belowFloor, receiptNone)
	activeNoToken.token = false
	enabledByInstall := dormantHost(belowFloor, receiptNone)
	enabledByInstall.enableOnInstall = true
	changedSource := dormantHost(belowFloor, receiptNone)
	changedSource.changedSource = true
	activeLegacySource := activeHost(belowFloor, receiptNone)
	activeLegacySource.legacySource = true
	dormantLegacySource := dormantHost(belowFloor, receiptNone)
	dormantLegacySource.legacySource = true
	halfInstalled := dormantHost(belowFloor, receiptNone)
	halfInstalled.dpkgState = "half-installed"
	runningOutside := dormantHost(belowFloor, receiptNone)
	runningOutside.pgrepStatus = "0"
	dormantNoUnit := dormantHost(belowFloor, receiptNone)
	dormantNoUnit.load, dormantNoUnit.unitFile = "not-found", ""
	for _, tc := range []struct {
		name                   string
		host                   unmanagedHost
		wantOK                 bool
		wantOut, wantRecheck   string
		wantInstall, wantStart bool
	}{
		{name: "absent", host: unmanagedHost{}, wantOK: true, wantOut: "cloudflared not installed", wantRecheck: "not installed"},
		{name: "active-below-floor", host: activeHost(belowFloor, receiptNone), wantOK: true, wantInstall: true, wantStart: true, wantRecheck: baseline.MinimumVersion + " active"},
		{name: "active-without-token", host: activeNoToken, wantOut: "reprovision the server"},
		{name: "dormant-below-floor", host: dormantHost(belowFloor, receiptNone), wantOK: true, wantInstall: true, wantOut: baseline.MinimumVersion + " dormant", wantRecheck: baseline.MinimumVersion + " dormant"},
		{name: "dormant-without-unit-or-token", host: dormantNoUnit, wantOK: true, wantInstall: true, wantRecheck: "dormant"},
		{name: "dormant-foreign-receipt", host: dormantHost(baseline.MinimumVersion, receiptForeign), wantOK: true, wantInstall: true, wantRecheck: "dormant"},
		{name: "dormant-enabled-by-install", host: enabledByInstall, wantInstall: true, wantOut: "no longer dormant"},
		{name: "dormant-changed-source", host: changedSource, wantOut: "apt source is missing or changed"},
		{name: "active-legacy-source", host: activeLegacySource, wantOK: true, wantInstall: true, wantStart: true, wantRecheck: baseline.MinimumVersion + " active"},
		{name: "dormant-legacy-source", host: dormantLegacySource, wantOK: true, wantInstall: true, wantRecheck: "dormant"},
		{name: "half-installed", host: halfInstalled, wantOut: "package status is half-installed"},
		{name: "process-outside-service", host: runningOutside, wantOut: "running outside cloudflared.service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newUnmanagedHarness(t, tc.host)
			out, err := h.repair(t)
			if tc.wantOK != (err == nil) {
				t.Fatalf("repair err = %v, wantOK %v\n%s", err, tc.wantOK, out)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("repair output = %q, want %q", out, tc.wantOut)
			}
			installed := strings.Contains(h.stateLog(t, "apt.log"), "install -y")
			if installed != tc.wantInstall {
				t.Fatalf("apt install ran = %v, want %v\n%s", installed, tc.wantInstall, h.stateLog(t, "apt.log"))
			}
			started := h.stateLog(t, "systemctl.log") != ""
			if started != tc.wantStart {
				t.Fatalf("service enable/restart ran = %v, want %v\n%s", started, tc.wantStart, h.stateLog(t, "systemctl.log"))
			}
			if tc.wantInstall && tc.wantOK {
				receipt, err := os.ReadFile(h.receiptPath)
				if want := baseline.MinimumVersion + " " + testReinstalledDigest + "\n"; err != nil || string(receipt) != want {
					t.Fatalf("receipt = %q, %v; want %q", receipt, err, want)
				}
			}
			if !tc.wantOK {
				return
			}
			// A repaired host must end on the current source line, whether it
			// started there or on the migrated legacy line.
			if source, err := os.ReadFile(filepath.Join(h.dir, "cloudflared.list")); tc.wantInstall && (err != nil || string(source) != cloudflaredAPTSource()+"\n") {
				t.Fatalf("apt source after repair = %q, %v; want %q", source, err, cloudflaredAPTSource())
			}
			recheck, err := h.check(t)
			if err != nil || !strings.Contains(recheck, tc.wantRecheck) {
				t.Fatalf("recheck after repair = %v\n%s\nwant %q", err, recheck, tc.wantRecheck)
			}
		})
	}
}
