package bootstraptools

// Executable regression tests for the canonical bootstrap shell helpers.
//
// The Go tests elsewhere in this package assert on the *text* of the generated
// script; they never run it. That leaves the pure, side-effect-free helpers
// (token validation, package parsing, atomic file install) unexercised. These
// tests source the canonical script with SERVERPRO_BOOTSTRAP_SOURCE_ONLY=1 so
// the privileged installer never runs, then invoke individual helpers and assert
// their real runtime behaviour. bats is not a project dependency, so bash is
// driven directly through os/exec, which also matches how CI runs the script.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	// canonicalScriptPath is the shell script under test, relative to this package.
	canonicalScriptPath = "serverpro-bootstrap-tools.sh"
	// sourceOnlyEnv suppresses the top-level main invocation while sourcing.
	sourceOnlyEnv = "SERVERPRO_BOOTSTRAP_SOURCE_ONLY=1"
	// minNamerefMajor/minNamerefMinor is the first bash release exposing
	// `local -n` namerefs (4.3); older bash (e.g. macOS system 3.2) cannot run
	// read_package_env, so the suite skips there rather than reporting a false
	// failure. CI runs bash 5.x and executes every case.
	minNamerefMajor = 4
	minNamerefMinor = 3
	// testDockerGPGFingerprint mirrors the reviewed key identity so executable
	// tests can prove exact primary-key set handling without network access.
	testDockerGPGFingerprint = "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
)

// bashSupportsNamerefs reports whether PATH's bash is new enough to expose
// `local -n` namerefs (>= 4.3). Keeping the command literal avoids teaching the
// test to execute arbitrary binaries from test data.
func bashSupportsNamerefs() bool {
	out, err := exec.Command("bash", "-c", "printf '%s %s' \"${BASH_VERSINFO[0]}\" \"${BASH_VERSINFO[1]}\"").Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return false
	}
	major, err1 := strconv.Atoi(fields[0])
	minor, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > minNamerefMajor || (major == minNamerefMajor && minor >= minNamerefMinor)
}

// requireBash skips the calling test when this host cannot run the shell suite.
func requireBash(t *testing.T) {
	t.Helper()
	if !bashSupportsNamerefs() {
		t.Skip("no bash >= 4.3 available")
	}
}

// runHelper sources the canonical script and runs the given bash snippet against
// it, returning the process exit code, stdout, and stderr. extraEnv entries are
// appended to the child environment; on duplicate keys os/exec keeps the last
// value, so test overrides simply append after manifestEnv().
func runHelper(t *testing.T, snippet string, extraEnv ...string) (int, string, string) {
	t.Helper()
	requireBash(t)
	abs, err := filepath.Abs(canonicalScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	// $1 carries the script path so the snippet can `source "$1"`; bash reads the
	// helper body from stdin so exec.Command never receives a dynamic `-c` string.
	program := `source "$1"; ` + snippet
	cmd := exec.Command("bash", "-s", "--", abs)
	cmd.Stdin = strings.NewReader(program)
	cmd.Env = append(os.Environ(), sourceOnlyEnv)
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running helper failed: %v (stderr: %s)", err, stderr.String())
		}
		code = exitErr.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

// manifestEnv returns the complete pinned manifest validate_bootstrap_env
// requires, derived from the same single authority remote delivery uses.
func manifestEnv() []string {
	pairs := manifestEnvPairs()
	env := make([]string, 0, len(pairs))
	for _, kv := range pairs {
		env = append(env, kv[0]+"="+kv[1])
	}
	return env
}

func TestShellVerifyGPGFingerprintRequiresExactPrimaryKeySet(t *testing.T) {
	requireBash(t)
	const subkeyFingerprint = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	cases := []struct {
		name    string
		records string
		wantOK  bool
	}{
		{
			name:    "approved-primary-with-subkey",
			records: "pub:::::::::\nfpr:::::::::" + testDockerGPGFingerprint + ":\nsub:::::::::\nfpr:::::::::" + subkeyFingerprint + ":\n",
			wantOK:  true,
		},
		{
			name:    "mismatched-primary",
			records: "pub:::::::::\nfpr:::::::::" + subkeyFingerprint + ":\n",
		},
		{
			name:    "additional-primary",
			records: "pub:::::::::\nfpr:::::::::" + testDockerGPGFingerprint + ":\npub:::::::::\nfpr:::::::::" + subkeyFingerprint + ":\n",
		},
		{
			name:    "subkey-only",
			records: "sub:::::::::\nfpr:::::::::" + testDockerGPGFingerprint + ":\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			keyPath := filepath.Join(dir, "key.gpg")
			if err := os.WriteFile(keyPath, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			fakeGPG := filepath.Join(binDir, "gpg")
			if err := os.WriteFile(fakeGPG, []byte("#!/bin/sh\nset -eu\nprintf '%s' \"$GPG_RECORDS\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			code, _, _ := runHelper(t,
				`verify_gpg_fingerprint "$KEYFILE" "$EXPECTED" docker`,
				"KEYFILE="+keyPath,
				"EXPECTED="+testDockerGPGFingerprint,
				"GPG_RECORDS="+tc.records,
				"PATH="+binDir+":"+os.Getenv("PATH"))
			if tc.wantOK && code != 0 {
				t.Fatalf("approved key set rejected with exit %d", code)
			}
			if !tc.wantOK && code == 0 {
				t.Fatal("unapproved primary-key set accepted")
			}
		})
	}
}

type validatorCase struct {
	name    string
	value   string
	wantErr bool
}

// assertShellValidator drives one shell token validator through its case
// matrix: every value must exit non-zero exactly when the case says reject.
func assertShellValidator(t *testing.T, snippet string, cases []validatorCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runHelper(t, snippet, "TESTVAL="+tc.value)
			if tc.wantErr && code == 0 {
				t.Fatalf("value %q: want rejection, got exit 0", tc.value)
			}
			if !tc.wantErr && code != 0 {
				t.Fatalf("value %q: want exit 0, got %d", tc.value, code)
			}
		})
	}
}

func TestShellValidateVersionToken(t *testing.T) {
	assertShellValidator(t, `validate_version_token NAME "$TESTVAL"`, []validatorCase{
		{"semver", "1.2.3", false},
		{"calver", "2026.6.14", false},
		{"prefixed", "v1.64.5", false},
		{"plus-build", "1.0.0+build", false},
		{"empty", "", true},
		{"space", "1.2 3", true},
		{"shell-meta", "1;rm", true},
		{"dollar", "1$x", true},
	})
}

func TestShellValidateSHA256Token(t *testing.T) {
	assertShellValidator(t, `validate_sha256_token NAME "$TESTVAL"`, []validatorCase{
		{"valid-64-hex", strings.Repeat("a", 64), false},
		{"uppercase-hex", strings.Repeat("F", 64), false},
		{"too-short", strings.Repeat("a", 63), true},
		{"too-long", strings.Repeat("a", 65), true},
		{"non-hex", strings.Repeat("g", 64), true},
		{"empty", "", true},
	})
}

// TestShellValidateUserToken pins the username sanitizer that runs before the
// value reaches sudo -u / getent / run_as_target. A broken character class here
// is a root command-injection vector, so every shell metacharacter must reject.
func TestShellValidateUserToken(t *testing.T) {
	assertShellValidator(t, `validate_user_token NAME "$TESTVAL"`, []validatorCase{
		{"simple", "deploy", false},
		{"hyphen", "my-user", false},
		{"underscore", "_deploy", false},
		{"dotted", "deploy.ops", false},
		{"alnum", "deploy123", false},
		{"empty", "", true},
		{"semicolon-injection", "deploy;rm", true},
		{"dollar", "deploy$HOME", true},
		{"backtick", "deploy`id`", true},
		{"pipe", "a|b", true},
		{"ampersand", "a&b", true},
		{"space", "de ploy", true},
		{"slash", "a/b", true},
		{"paren", "a(b", true},
		{"newline", "deploy\n", true},
	})
}

func TestShellValidateMiseBackend(t *testing.T) {
	assertShellValidator(t, `validate_mise_backend NAME "$TESTVAL"`, []validatorCase{
		{"github", "github:ogulcancelik/herdr", false},
		{"missing-backend", "ogulcancelik/herdr", true},
		{"missing-repository", "github:ogulcancelik", true},
		{"extra-path", "github:ogulcancelik/herdr/release", true},
		{"shell-metacharacter", "github:owner/repo;id", true},
	})
}

func TestShellValidateMiseToolBackend(t *testing.T) {
	assertShellValidator(t, `validate_mise_tool_backend NAME "$TESTVAL"`, []validatorCase{
		{"aqua-repository", "aqua:astral-sh/uv", false},
		{"core-tool", "core:rust", false},
		{"github-release", "github:Ataraxy-Labs/sem", false},
		{"missing-source", "core:", true},
		{"unsupported-backend", "asdf:rust", true},
		{"absolute-source", "aqua:/astral-sh/uv", true},
		{"duplicate-slash", "aqua:astral-sh//uv", true},
		{"shell-metacharacter", "core:rust;id", true},
	})
}

func TestShellValidateManagedMiseManifestRejectsStructuralDrift(t *testing.T) {
	const (
		backendEnvField     = 3
		checksumX64EnvField = 8
		forceRepairField    = 10
	)
	manifest := managedMiseManifest()
	rows := strings.Split(manifest, "\n")
	withLastRowField := func(index int, value string) string {
		changedRows := slices.Clone(rows)
		fields := strings.Split(changedRows[len(changedRows)-1], "|")
		fields[index] = value
		changedRows[len(changedRows)-1] = strings.Join(fields, "|")
		return strings.Join(changedRows, "\n")
	}
	cases := []struct {
		name     string
		manifest string
		wantOK   bool
	}{
		{name: "canonical", manifest: manifest, wantOK: true},
		{name: "duplicate", manifest: manifest + "\n" + rows[0]},
		{name: "missing", manifest: strings.Join(rows[:len(rows)-1], "\n")},
		{name: "probe-mismatch", manifest: strings.Replace(manifest, "|false|uv", "|false|node", 1)},
		{name: "invalid-force", manifest: strings.Replace(manifest, "|true|rust", "|sometimes|rust", 1)},
		{name: "unpaired-checksum", manifest: withLastRowField(checksumX64EnvField, "-")},
		{name: "checksum-without-github-backend", manifest: withLastRowField(backendEnvField, "SERVERPRO_BOOTSTRAP_UV_BACKEND")},
		{name: "checksum-without-forced-repair", manifest: withLastRowField(forceRepairField, "false")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := append(manifestEnv(), managedMiseManifestEnv+"="+tc.manifest)
			code, _, _ := runHelper(t, `validate_managed_mise_manifest`, env...)
			if tc.wantOK && code != 0 {
				t.Fatal("canonical manifest rejected")
			}
			if !tc.wantOK && code == 0 {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestShellValidateManagedReleaseChecksums(t *testing.T) {
	for _, tc := range []struct {
		name    string
		envName string
		value   string
		wantErr bool
	}{
		{name: "valid", envName: "SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_X64", value: InspectLinuxX64SHA256},
		{name: "short", envName: "SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_X64", value: strings.Repeat("a", 63), wantErr: true},
		{name: "non-hex", envName: "SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_X64", value: strings.Repeat("z", 64), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := append(manifestEnv(), tc.envName+"="+tc.value)
			code, _, _ := runHelper(t, `validate_managed_mise_manifest`, env...)
			if tc.wantErr && code == 0 {
				t.Fatal("invalid managed release checksum accepted")
			}
			if !tc.wantErr && code != 0 {
				t.Fatal("valid managed release checksum rejected")
			}
		})
	}
}

func TestShellManagedMiseToolSHA256ForArch(t *testing.T) {
	row := managedMiseToolRowForTest(t, "inspect")
	for _, tc := range []struct {
		name    string
		arch    string
		want    string
		wantErr bool
	}{
		{name: "x86-64", arch: "x86_64", want: InspectLinuxX64SHA256},
		{name: "aarch64", arch: "aarch64", want: InspectLinuxArm64SHA256},
		{name: "arm64", arch: "arm64", want: InspectLinuxArm64SHA256},
		{name: "unsupported", arch: "armv7l", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, _ := runHelper(t, `managed_mise_tool_sha256_for_arch "$TESTROW" "$TESTARCH"`, append(manifestEnv(), "TESTROW="+row, "TESTARCH="+tc.arch)...)
			if tc.wantErr {
				if code == 0 {
					t.Fatalf("arch %q: want rejection, got %q", tc.arch, stdout)
				}
				return
			}
			if code != 0 || stdout != tc.want {
				t.Fatalf("arch %q: got code=%d sha=%q, want %q", tc.arch, code, stdout, tc.want)
			}
		})
	}
}

func managedMiseToolRowForTest(t *testing.T, key string) string {
	t.Helper()
	for _, row := range strings.Split(managedMiseManifest(), "\n") {
		if strings.HasPrefix(row, key+"|") {
			return row
		}
	}
	t.Fatalf("managed mise manifest missing %s", key)
	return ""
}

func TestShellValidateRustProfile(t *testing.T) {
	cases := []validatorCase{
		{"default", "default", false},
		{"minimal", "minimal", true},
		{"complete", "complete", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := append(manifestEnv(), "SERVERPRO_BOOTSTRAP_RUST_PROFILE="+tc.value)
			code, _, _ := runHelper(t, `validate_managed_mise_manifest`, env...)
			if tc.wantErr && code == 0 {
				t.Fatalf("profile %q unexpectedly passed", tc.value)
			}
			if !tc.wantErr && code != 0 {
				t.Fatalf("profile %q unexpectedly failed", tc.value)
			}
		})
	}
}

// TestShellValidateToolName guards the pi tool-name token before it reaches
// `npm install -g`. Scoped npm names (@scope/pkg) and dotted/suffixed names
// pass; shell metacharacters reject.
func TestShellValidateToolName(t *testing.T) {
	assertShellValidator(t, `validate_tool_name NAME "$TESTVAL"`, []validatorCase{
		{"scoped", "@scope/pkg", false},
		{"plain", "ripgrep", false},
		{"hyphenated", "pi-coding-agent", false},
		{"dotted", "pkg.name", false},
		{"plus-suffix", "pkg+beta", false},
		{"empty", "", true},
		{"semicolon", "pkg;rm", true},
		{"dollar", "pkg$x", true},
		{"pipe", "pkg|x", true},
		{"space", "pk g", true},
	})
}

func TestShellValidatePackageToken(t *testing.T) {
	assertShellValidator(t, `validate_package_token "$TESTVAL"`, []validatorCase{
		{"apt-simple", "apt:git", false},
		{"apt-dotted", "apt:docker-ce.cli", false},
		{"missing-prefix", "git", true},
		{"wrong-prefix", "npm:foo", true},
		{"empty-name", "apt:", true},
		{"shell-meta", "apt:git;rm", true},
	})
}

func TestShellHerdrSHA256ForArch(t *testing.T) {
	cases := []struct {
		name    string
		arch    string
		want    string
		wantErr bool
	}{
		{name: "x86-64", arch: "x86_64", want: HerdrLinuxX64SHA256},
		{name: "aarch64", arch: "aarch64", want: HerdrLinuxArm64SHA256},
		{name: "arm64", arch: "arm64", want: HerdrLinuxArm64SHA256},
		{name: "unsupported", arch: "armv7l", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, _ := runHelper(t,
				`bootstrap_herdr_sha256_for_arch "$TESTARCH"`,
				"TESTARCH="+tc.arch,
				"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64="+HerdrLinuxX64SHA256,
				"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64="+HerdrLinuxArm64SHA256)
			if tc.wantErr {
				if code == 0 {
					t.Fatalf("arch %q: want failure, got %q", tc.arch, stdout)
				}
				return
			}
			if code != 0 || stdout != tc.want {
				t.Fatalf("arch %q: got code=%d sha=%q, want %q", tc.arch, code, stdout, tc.want)
			}
		})
	}
}

func TestShellReadPackageEnvParsesTokens(t *testing.T) {
	code, stdout, stderr := runHelper(t,
		`declare -a pkgs; read_package_env SERVERPRO_BOOTSTRAP_GIT_PACKAGES pkgs; printf '%s\n' "${#pkgs[@]}" "${pkgs[0]}" "${pkgs[1]}"`,
		"SERVERPRO_BOOTSTRAP_GIT_PACKAGES=apt:git apt:openssh-client")
	if code != 0 {
		t.Fatalf("read_package_env failed: exit %d, stderr: %s", code, stderr)
	}
	got := strings.Fields(stdout)
	want := []string{"2", "apt:git", "apt:openssh-client"}
	if len(got) != len(want) {
		t.Fatalf("unexpected output %q", stdout)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("field %d: got %q, want %q (full: %q)", i, got[i], want[i], stdout)
		}
	}
}

func TestShellReadPackageEnvRejectsEmpty(t *testing.T) {
	code, _, _ := runHelper(t,
		`declare -a pkgs; read_package_env SERVERPRO_BOOTSTRAP_GIT_PACKAGES pkgs`,
		"SERVERPRO_BOOTSTRAP_GIT_PACKAGES=   ")
	if code == 0 {
		t.Fatal("read_package_env accepted whitespace-only package list")
	}
}

// TestShellReadPackageEnvNamerefCollision pins the regression: a caller whose
// array is named `out` must not trigger a bash circular name reference. Before
// the fix the nameref parameter itself was `out`, so this aborted under set -u.
func TestShellReadPackageEnvNamerefCollision(t *testing.T) {
	code, stdout, stderr := runHelper(t,
		`declare -a out; read_package_env SERVERPRO_BOOTSTRAP_GIT_PACKAGES out; printf '%s\n' "${#out[@]}" "${out[0]}"`,
		"SERVERPRO_BOOTSTRAP_GIT_PACKAGES=apt:git apt:curl")
	if code != 0 {
		t.Fatalf("nameref collision regression: exit %d, stderr: %s", code, stderr)
	}
	if strings.Contains(stderr, "circular name reference") {
		t.Fatalf("nameref collision regression: %s", stderr)
	}
	got := strings.Fields(stdout)
	if len(got) != 2 || got[0] != "2" || got[1] != "apt:git" {
		t.Fatalf("unexpected output %q", stdout)
	}
}

// atomicInstallOwnerEnv returns TESTOWNER/TESTGROUP env entries for the current
// user so atomic_install_file's `install -o/-g` needs no privilege in tests.
func atomicInstallOwnerEnv(t *testing.T) (string, string) {
	t.Helper()
	cur, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	grp, err := user.LookupGroupId(cur.Gid)
	if err != nil {
		t.Fatal(err)
	}
	return "TESTOWNER=" + cur.Username, "TESTGROUP=" + grp.Name
}

func TestShellAtomicInstallFile(t *testing.T) {
	requireBash(t)
	t.Run("installs-new-file", testAtomicInstallInstallsNew)
	t.Run("noop-when-identical", testAtomicInstallNoopIdentical)
	t.Run("fails-when-source-missing", testAtomicInstallFailsSourceMissing)
}

func testAtomicInstallInstallsNew(t *testing.T) {
	ownerEnv, groupEnv := atomicInstallOwnerEnv(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runHelper(t,
		`atomic_install_file "$SRC" "$DST" 0644 "$TESTOWNER" "$TESTGROUP"; cat "$DST"`,
		"SRC="+src, "DST="+dst, ownerEnv, groupEnv)
	if code != 0 {
		t.Fatalf("expected exit 0 when installing new file, got %d (stderr: %s)", code, stderr)
	}
	if stdout != "payload\n" {
		t.Fatalf("destination content = %q, want %q", stdout, "payload\n")
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("destination mode = %o, want 0644", info.Mode().Perm())
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source temp file was not cleaned up")
	}
}

func testAtomicInstallNoopIdentical(t *testing.T) {
	ownerEnv, groupEnv := atomicInstallOwnerEnv(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("same\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("same\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Identical content must return 1 (no change) and remove the source.
	code, _, _ := runHelper(t,
		`atomic_install_file "$SRC" "$DST" 0644 "$TESTOWNER" "$TESTGROUP"`,
		"SRC="+src, "DST="+dst, ownerEnv, groupEnv)
	if code != 1 {
		t.Fatalf("expected exit 1 (no change) for identical content, got %d", code)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source temp file was not cleaned up on no-op path")
	}
	// No-op must leave destination content untouched. Verify via the shell `cat`
	// (runHelper stdout) instead of os.ReadFile to dodge a gosec path-taint flag on
	// a test reading its own temp file.
	_, stdout, _ := runHelper(t, `cat "$DST"`, "DST="+dst)
	if stdout != "same\n" {
		t.Fatalf("no-op rewrote dst = %q, want %q", stdout, "same\n")
	}
}

func testAtomicInstallFailsSourceMissing(t *testing.T) {
	ownerEnv, groupEnv := atomicInstallOwnerEnv(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	code, _, _ := runHelper(t,
		`atomic_install_file "$SRC" "$DST" 0644 "$TESTOWNER" "$TESTGROUP"`,
		"SRC="+filepath.Join(dir, "missing"), "DST="+dst, ownerEnv, groupEnv)
	if code == 0 {
		t.Fatal("expected failure when source is missing, got exit 0")
	}
}

// miseVersionCases is the shared version matrix for every mise version reader:
// root readiness, target-user readiness, and the doctor check must all apply
// the same release-date stripping semantics.
var miseVersionCases = []struct {
	name    string
	version string
	wantOK  bool
}{
	{"equal", MinimumMiseVersion, true},
	{"release-date-suffix", MinimumMiseVersion + " (2026-07-23)", true},
	{"newer", "2026.8.0", true},
	{"older", "2026.7.17", false},
}

// writeFakeMise installs a stub mise at path that reports the given version and
// accepts every other subcommand (e.g. `bootstrap --help`).
func writeFakeMise(t *testing.T, path, version string) {
	t.Helper()
	stub := "#!/bin/sh\n" +
		"if [ \"${1:-}\" = --version ]; then printf 'mise " + version + "\\n'; exit 0; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeMiseHome creates a HOME layout with .local/bin/mise reporting version.
func fakeMiseHome(t *testing.T, version string) (home, binDir string) {
	t.Helper()
	home = t.TempDir()
	binDir = filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeMise(t, filepath.Join(binDir, "mise"), version)
	return home, binDir
}

func TestShellTargetUVReadyVersionMatrix(t *testing.T) {
	cases := []struct {
		name    string
		version string
		wantOK  bool
	}{
		{name: "exact", version: UVVersion, wantOK: true},
		{name: "release-metadata", version: UVVersion + " (fixture build metadata)", wantOK: true},
		{name: "older", version: "0.11.0"},
		{name: "newer", version: "0.13.0"},
		{name: "malformed", version: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			miseBin := filepath.Join(home, ".local", "bin")
			toolBin := filepath.Join(home, "tools")
			for _, dir := range []string{miseBin, toolBin} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			miseScript := "#!/bin/sh\nshift\n[ \"${1:-}\" != -- ] || shift\nexec \"$@\"\n"
			if err := os.WriteFile(filepath.Join(miseBin, "mise"), []byte(miseScript), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(toolBin, "uv"), []byte("#!/bin/sh\nprintf 'uv %s\\n' \"$FAKE_UV_VERSION\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := runHelper(t, `
TARGET_USER=deploy
run_as_target() { HOME="$FAKE_HOME" PATH="$FAKE_TOOL_BIN:$PATH" bash -c "$1"; }
ready=0; target_managed_mise_tool_ready "$(managed_mise_tool_row uv)" >/dev/null || ready=$?
verified=0; verify_managed_mise_tool uv >/dev/null || verified=$?
printf '%s %s' "$ready" "$verified"
`, append(manifestEnv(), "FAKE_HOME="+home, "FAKE_TOOL_BIN="+toolBin, "FAKE_UV_VERSION="+tc.version)...)
			if code != 0 {
				t.Fatalf("uv matrix harness failed: %d (stderr: %s)", code, stderr)
			}
			want := "1 1"
			if tc.wantOK {
				want = "0 0"
			}
			if stdout != want {
				t.Fatalf("uv %q readiness/verification = %q, want %q", tc.version, stdout, want)
			}
		})
	}
}

func TestShellTargetRustReadyComponentMatrix(t *testing.T) {
	cases := []struct {
		name          string
		rustVersion   string
		cargoStatus   string
		rustfmtStatus string
		clippyStatus  string
		components    string
		wantOK        bool
	}{
		{name: "default-profile", rustVersion: RustVersion, components: "rust-docs-x86_64-unknown-linux-gnu (installed)", wantOK: true},
		{name: "wrong-rustc", rustVersion: "1.96.0", components: "rust-docs-x86_64-unknown-linux-gnu (installed)"},
		{name: "missing-cargo", rustVersion: RustVersion, cargoStatus: "1", components: "rust-docs-x86_64-unknown-linux-gnu (installed)"},
		{name: "missing-rustfmt", rustVersion: RustVersion, rustfmtStatus: "1", components: "rust-docs-x86_64-unknown-linux-gnu (installed)"},
		{name: "missing-clippy", rustVersion: RustVersion, clippyStatus: "1", components: "rust-docs-x86_64-unknown-linux-gnu (installed)"},
		{name: "missing-docs", rustVersion: RustVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			miseBin := filepath.Join(home, ".local", "bin")
			toolBin := filepath.Join(home, "tools")
			for _, dir := range []string{miseBin, toolBin} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			scripts := map[string]string{
				"mise":    "#!/bin/sh\nshift\n[ \"${1:-}\" != -- ] || shift\nexec \"$@\"\n",
				"rustc":   "#!/bin/sh\nprintf 'rustc %s (fixture)\\n' \"$FAKE_RUST_VERSION\"\n",
				"cargo":   "#!/bin/sh\nif [ \"${1:-}\" = clippy ]; then exit \"${FAKE_CLIPPY_STATUS:-0}\"; fi\nexit \"${FAKE_CARGO_STATUS:-0}\"\n",
				"rustfmt": "#!/bin/sh\nexit \"${FAKE_RUSTFMT_STATUS:-0}\"\n",
				"rustup":  "#!/bin/sh\nprintf '%s\\n' \"${FAKE_RUST_COMPONENTS:-}\"\n",
			}
			for name, script := range scripts {
				path := filepath.Join(toolBin, name)
				if name == "mise" {
					path = filepath.Join(miseBin, name)
				}
				if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			code, stdout, stderr := runHelper(t, `
TARGET_USER=deploy
run_as_target() { HOME="$FAKE_HOME" PATH="$FAKE_TOOL_BIN:$PATH" bash -c "$1"; }
ready=0; target_managed_mise_tool_ready "$(managed_mise_tool_row rust)" >/dev/null || ready=$?
verified=0; verify_managed_mise_tool rust >/dev/null || verified=$?
printf '%s %s' "$ready" "$verified"
`, append(manifestEnv(),
				"FAKE_HOME="+home,
				"FAKE_TOOL_BIN="+toolBin,
				"FAKE_RUST_VERSION="+tc.rustVersion,
				"FAKE_CARGO_STATUS="+tc.cargoStatus,
				"FAKE_RUSTFMT_STATUS="+tc.rustfmtStatus,
				"FAKE_CLIPPY_STATUS="+tc.clippyStatus,
				"FAKE_RUST_COMPONENTS="+tc.components,
			)...)
			if code != 0 {
				t.Fatalf("rust matrix harness failed: %d (stderr: %s)", code, stderr)
			}
			want := "1 1"
			if tc.wantOK {
				want = "0 0"
			}
			if stdout != want {
				t.Fatalf("rust readiness/verification = %q, want %q", stdout, want)
			}
		})
	}
}

func TestManagedMiseDoctorProbeMatrix(t *testing.T) {
	home := t.TempDir()
	miseDir := filepath.Join(home, ".local", "bin")
	toolDir := filepath.Join(home, "tools")
	for _, dir := range []string{miseDir, toolDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scripts := map[string]string{
		"mise":      "#!/bin/sh\nshift\n[ \"${1:-}\" != -- ] || shift\nexec \"$@\"\n",
		"node":      "#!/bin/sh\nprintf 'v%s\\n' \"$FAKE_NODE_VERSION\"\n",
		"npm":       "#!/bin/sh\nprintf '11.0.0\\n'\n",
		"uv":        "#!/bin/sh\nprintf 'uv %s (fixture metadata)\\n' \"$FAKE_UV_VERSION\"\n",
		"rustc":     "#!/bin/sh\nprintf 'rustc %s (fixture)\\n' \"$FAKE_RUST_VERSION\"\n",
		"cargo":     "#!/bin/sh\nif [ \"${1:-}\" = clippy ]; then printf 'clippy 0.1.97\\n'; else printf 'cargo 1.97.1\\n'; fi\n",
		"rustfmt":   "#!/bin/sh\nprintf 'rustfmt 1.8.0\\n'\n",
		"rustup":    "#!/bin/sh\nprintf 'rust-docs-x86_64-unknown-linux-gnu (installed)\\n'\n",
		"tmux":      "#!/bin/sh\nprintf 'tmux %s\\n' \"$FAKE_TMUX_VERSION\"\n",
		"gh":        "#!/bin/sh\nprintf 'gh version %s (fixture)\\n' \"$FAKE_GH_VERSION\"\n",
		"rg":        "#!/bin/sh\nprintf 'ripgrep %s (fixture)\\n' \"$FAKE_RG_VERSION\"\n",
		"fd":        "#!/bin/sh\nprintf 'fd %s\\n' \"$FAKE_FD_VERSION\"\n",
		"ast-grep":  "#!/bin/sh\nprintf 'ast-grep %s (fixture)\\n' \"$FAKE_AST_GREP_VERSION\"\n",
		"sem":       "#!/bin/sh\nprintf 'sem %s\\n' \"$FAKE_SEM_VERSION\"\n",
		"inspect":   "#!/bin/sh\nprintf 'Entity-level code review\\n'\n",
		"uname":     "#!/bin/sh\nprintf 'x86_64\\n'\n",
		"sha256sum": "#!/bin/sh\nprintf '%s  %s\\n' \"$FAKE_INSPECT_SHA\" \"${1:-}\"\n",
	}
	for name, script := range scripts {
		dir := toolDir
		if name == "mise" {
			dir = miseDir
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	valid := map[string]string{
		"node":     NodeVersion,
		"uv":       UVVersion,
		"rust":     RustVersion,
		"tmux":     TmuxVersion,
		"gh":       GitHubCLIVersion,
		"rg":       RipgrepVersion,
		"fd":       FdVersion,
		"ast-grep": AstGrepVersion,
		"sem":      SemVersion,
		"inspect":  InspectLinuxX64SHA256,
	}
	envName := map[string]string{
		"node":     "FAKE_NODE_VERSION",
		"uv":       "FAKE_UV_VERSION",
		"rust":     "FAKE_RUST_VERSION",
		"tmux":     "FAKE_TMUX_VERSION",
		"gh":       "FAKE_GH_VERSION",
		"rg":       "FAKE_RG_VERSION",
		"fd":       "FAKE_FD_VERSION",
		"ast-grep": "FAKE_AST_GREP_VERSION",
		"sem":      "FAKE_SEM_VERSION",
		"inspect":  "FAKE_INSPECT_SHA",
	}
	baseEnv := append(os.Environ(), "HOME="+home, "PATH="+toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for key, version := range valid {
		baseEnv = append(baseEnv, envName[key]+"="+version)
	}
	for _, tool := range managedMiseTools {
		validValue := tool.version
		if tool.key == "inspect" {
			validValue = InspectLinuxX64SHA256
		}
		for _, tc := range []struct {
			name    string
			version string
			wantOK  bool
		}{
			{name: "valid", version: validValue, wantOK: true},
			{name: "wrong-version", version: "0.0.0"},
		} {
			t.Run(tool.key+"/"+tc.name, func(t *testing.T) {
				cmd := exec.Command("bash", "-c", managedMiseProbeCommand(tool))
				cmd.Env = append(baseEnv, envName[tool.key]+"="+tc.version)
				out, err := cmd.CombinedOutput()
				if tc.wantOK && err != nil {
					t.Fatalf("valid probe failed: %v: %s", err, out)
				}
				if !tc.wantOK && err == nil {
					t.Fatalf("wrong version passed: %s", out)
				}
			})
		}
	}
}

// TestShellVersionAtLeast is the regression guard for the mise minimum-version
// gate. `mise --version` prints "mise <version>[( release-date)]"; the gate
// must compare the version token, not the literal "mise" or the date. The
// snippet mirrors the exact production parse used by
// mise_binary_bootstrap_capable, target_mise_ready, and miseCheckCommand: a
// drifted test parse (e.g. ${current##* }) can pass for the wrong reason.
func TestShellVersionAtLeast(t *testing.T) {
	cases := []struct {
		name        string
		miseVersion string
		wantOK      bool
	}{
		{"equal-minimum", MinimumMiseVersion, true},
		{"release-date-suffix", MinimumMiseVersion + " (2026-07-23)", true},
		{"newer-patch", "2026.7.19", true},
		{"newer-minor", "2026.8.1", true},
		{"newer-year", "2027.1.1", true},
		{"older-patch", "2026.7.17", false},
		{"older-with-release-date", "2026.7.17 (2026-07-30)", false},
		{"ancient", "2020.1.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runHelper(t,
				`current="mise `+tc.miseVersion+`"; current=${current#mise }; current=${current%% *}; version_at_least "$current" "`+MinimumMiseVersion+`"`)
			if tc.wantOK && code != 0 {
				t.Fatalf("version %q: want pass against min %q, got exit %d", tc.miseVersion, MinimumMiseVersion, code)
			}
			if !tc.wantOK && code == 0 {
				t.Fatalf("version %q: want reject against min %q, got exit 0", tc.miseVersion, MinimumMiseVersion)
			}
		})
	}
}

// TestShellTargetMiseReadyVersionMatrix executes the production target-user
// readiness probe against a fake mise and pins the release-date parse.
func TestShellTargetMiseReadyVersionMatrix(t *testing.T) {
	for _, tc := range miseVersionCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHome, _ := fakeMiseHome(t, tc.version)
			code, _, stderr := runHelper(t, `
TARGET_USER=deploy
run_as_target() { HOME="$FAKE_HOME" bash -c "$1"; }
target_mise_ready
`, append(manifestEnv(), "FAKE_HOME="+fakeHome)...)
			if tc.wantOK && code != 0 {
				t.Fatalf("version %q: want ready, got %d (stderr: %s)", tc.version, code, stderr)
			}
			if !tc.wantOK && code == 0 {
				t.Fatalf("version %q: want not-ready, got exit 0", tc.version)
			}
		})
	}
}

// TestShellMiseBinaryBootstrapCapableVersionMatrix pins the root-readiness
// capability gate across the same version matrix.
func TestShellMiseBinaryBootstrapCapableVersionMatrix(t *testing.T) {
	for _, tc := range miseVersionCases {
		t.Run(tc.name, func(t *testing.T) {
			fake := filepath.Join(t.TempDir(), "mise")
			writeFakeMise(t, fake, tc.version)
			code, _, _ := runHelper(t,
				`mise_binary_bootstrap_capable "$FAKE_MISE"`,
				"FAKE_MISE="+fake,
				"SERVERPRO_BOOTSTRAP_MIN_MISE_VERSION="+MinimumMiseVersion)
			if tc.wantOK && code != 0 {
				t.Fatalf("version %q: want capable, got %d", tc.version, code)
			}
			if !tc.wantOK && code == 0 {
				t.Fatalf("version %q: want rejection, got exit 0", tc.version)
			}
		})
	}
}

// TestShellMiseCheckCommandVersionMatrix executes the exact doctor mise check
// command against a fake mise so the doctor parse cannot drift from the
// bootstrap parse (same stripping semantics in every version reader).
func TestShellMiseCheckCommandVersionMatrix(t *testing.T) {
	for _, tc := range miseVersionCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHome, binDir := fakeMiseHome(t, tc.version)
			env := os.Environ()
			for i, entry := range env {
				if strings.HasPrefix(entry, "HOME=") {
					env[i] = "HOME=" + fakeHome
				}
				if strings.HasPrefix(entry, "PATH=") {
					env[i] = "PATH=" + binDir + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
				}
			}
			cmd := exec.Command("bash", "-c", "set -euo pipefail; cd \"$HOME\"; "+miseCheckCommand())
			cmd.Env = env
			err := cmd.Run()
			if tc.wantOK && err != nil {
				t.Fatalf("version %q: doctor check failed: %v", tc.version, err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("version %q: doctor check passed unexpectedly", tc.version)
			}
		})
	}
}

// fetchFixtureStubs fakes download, checksum, extraction, and install stages so
// fetch_verified_mise_binary runs hermetically; tests override one stage to
// exercise a specific failure gate.
const fetchFixtureStubs = `
bootstrap_min_mise_version() { printf '2026.7.18'; }
mise_release_arch() { printf 'x64'; }
bootstrap_sha256_env() { printf '%064d' 0; }
mktemp() {
  if [[ ${1:-} == -d ]]; then
    mkdir -p "$TEST_TMP/download"
    printf '%s' "$TEST_TMP/download"
  else
    : >"$TEST_TMP/published"
    printf '%s' "$TEST_TMP/published"
  fi
}
curl() {
  local out
  while [[ $# -gt 0 ]]; do
    if [[ $1 == -o ]]; then out=$2; break; fi
    shift
  done
  printf 'archive' >"$out"
}
sha256sum() { printf 'mise-v2026.7.18-linux-x64.tar.gz: OK\n'; }
tar() {
  mkdir -p "$TEST_TMP/download/mise/bin"
  printf 'binary' >"$TEST_TMP/download/mise/bin/mise"
}
install() {
  local -a args=("$@")
  local last=$((${#args[@]} - 1))
  cp "${args[$((last - 1))]}" "${args[$last]}"
}
`

// TestFetchVerifiedMiseBinaryReturnsOnlyPath pins a live-host regression:
// sha256sum -c writes a success line to stdout, while callers use command
// substitution and require stdout to contain only the verified binary path.
func TestFetchVerifiedMiseBinaryReturnsOnlyPath(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runHelper(t, fetchFixtureStubs+`
actual=$(fetch_verified_mise_binary)
printf '%s' "$actual"
`, "TEST_TMP="+dir)
	if code != 0 {
		t.Fatalf("fetch_verified_mise_binary failed: exit %d, stderr: %s", code, stderr)
	}
	want := filepath.Join(dir, "published")
	if stdout != want {
		t.Fatalf("fetch output = %q, want only %q", stdout, want)
	}
}

// TestFetchVerifiedMiseBinaryFailurePaths proves every download, checksum, and
// extraction failure gate aborts without publishing a path and without leaving
// temp state behind (audit: failure-path proof for the verified fetch).
func TestFetchVerifiedMiseBinaryFailurePaths(t *testing.T) {
	cases := []struct {
		name string
		stub string
	}{
		{"curl-fails", "curl() { return 1; }"},
		{"checksum-fails", "sha256sum() { return 1; }"},
		{"extract-fails", "tar() { return 1; }"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, stdout, _ := runHelper(t, fetchFixtureStubs+tc.stub+`
if actual=$(fetch_verified_mise_binary); then rc=0; else rc=$?; fi
printf 'rc=%d out=[%s]' "$rc" "$actual"
`, "TEST_TMP="+dir)
			if strings.HasPrefix(stdout, "rc=0 ") {
				t.Fatalf("%s: fetch succeeded unexpectedly: %s", tc.name, stdout)
			}
			if !strings.Contains(stdout, "out=[]") {
				t.Fatalf("%s: failure leaked a published path: %s", tc.name, stdout)
			}
			for _, leftover := range []string{"download", "published"} {
				if _, err := os.Stat(filepath.Join(dir, leftover)); !os.IsNotExist(err) {
					t.Fatalf("%s: temp state %q left behind", tc.name, leftover)
				}
			}
		})
	}
}

// TestShellValidateBootstrapEnvRejectsUnsupportedHerdrArch pins the audit fix:
// the default `all` target must fail before any host mutation when the machine
// architecture has no pinned Herdr release binary.
func TestShellValidateBootstrapEnvRejectsUnsupportedHerdrArch(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		arch    string
		wantErr bool
	}{
		{"all-x86_64", "all", "x86_64", false},
		{"all-arm64", "all", "arm64", false},
		{"all-armv7l", "all", "armv7l", true},
		{"all-riscv64", "all", "riscv64", true},
		{"pi-armv7l", "pi", "armv7l", false},
		{"mise-armv7l", "mise", "armv7l", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := append(manifestEnv(), "TESTTARGET="+tc.target, "TESTARCH="+tc.arch)
			code, _, stderr := runHelper(t,
				`BOOTSTRAP_TARGET="$TESTTARGET"; uname() { printf '%s' "$TESTARCH"; }; validate_bootstrap_env`,
				env...)
			if tc.wantErr && code == 0 {
				t.Fatalf("target %s arch %s: want rejection before mutation, got exit 0", tc.target, tc.arch)
			}
			if !tc.wantErr && code != 0 {
				t.Fatalf("target %s arch %s: want exit 0, got %d (stderr: %s)", tc.target, tc.arch, code, stderr)
			}
		})
	}
}

func TestManagedPackageUpdatesCommandReportsSimulationState(t *testing.T) {
	binDir := t.TempDir()
	fakeApt := filepath.Join(binDir, "apt-get")
	if err := os.WriteFile(fakeApt, []byte("#!/bin/sh\nprintf '%s\\n' \"$FAKE_APT_OUTPUT\"\nexit \"$FAKE_APT_STATUS\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		output     string
		status     string
		wantErr    bool
		wantOutput string
	}{
		{name: "current", output: "0 upgraded, 0 newly installed", status: "0", wantOutput: "current"},
		{name: "update-available", output: "Inst git [1:2.43.0] (1:2.44.0 stable)", status: "0", wantErr: true, wantOutput: "managed package updates available"},
		{name: "simulation-fails", output: "package metadata unavailable", status: "100", wantErr: true, wantOutput: "package metadata unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", managedPackageUpdatesCommand())
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FAKE_APT_OUTPUT="+tc.output,
				"FAKE_APT_STATUS="+tc.status,
			)
			out, err := cmd.CombinedOutput()
			if tc.wantErr && err == nil {
				t.Fatalf("expected failure, got output %q", out)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected success, got %v: %s", err, out)
			}
			if !strings.Contains(string(out), tc.wantOutput) {
				t.Fatalf("output %q missing %q", out, tc.wantOutput)
			}
		})
	}
}

func TestConfigureUserToolsForTargetExecutesCanonicalSettings(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	miseLog := filepath.Join(home, "mise.log")
	fakeMise := "#!/bin/sh\nset -eu\nif [ \"${1:-}\" = config ] && [ \"${2:-}\" = get ]; then exit 1; fi\nprintf '%s\\n' \"$*\" >>\"$MISE_LOG\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "mise"), []byte(fakeMise), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runHelper(t, `
TARGET_USER=deploy
TARGET_HOME="$FAKE_HOME"
ensure_mise_config_file() { :; }
run_as_target() { HOME="$FAKE_HOME" PATH="$FAKE_BIN:$PATH" MISE_LOG="$MISE_LOG" bash -c "$1"; }
uname() { printf 'x86_64'; }
configure_user_tools_for_target all
`, append(manifestEnv(), "FAKE_HOME="+home, "FAKE_BIN="+binDir, "MISE_LOG="+miseLog)...)
	if code != 0 {
		t.Fatalf("configure managed tools failed: %s", stderr)
	}
	config := filepath.Join(home, ".config", "mise", "config.toml")
	want := []string{
		"config set --file " + config + " tools.node " + NodeVersion,
		"config set --file " + config + " tool_alias.uv " + UVMiseBackend,
		"config set --file " + config + " tools.uv " + UVVersion,
		"config set --file " + config + " tool_alias.rust " + RustMiseBackend,
		"config set --file " + config + " tools.rust.version " + RustVersion,
		"config set --file " + config + " tools.rust.profile " + RustProfile,
		"config set --file " + config + " tools.tmux " + TmuxVersion,
		"config set --file " + config + " tools.gh " + GitHubCLIVersion,
		"config set --file " + config + " tools.rg " + RipgrepVersion,
		"config set --file " + config + " tools.fd " + FdVersion,
		"config set --file " + config + " tool_alias.ast-grep " + AstGrepMiseBackend,
		"config set --file " + config + " tools.ast-grep.version " + AstGrepVersion,
		"config set --file " + config + " tools.ast-grep.checksum sha256:" + AstGrepLinuxX64SHA256,
		"config set --file " + config + " tool_alias.sem " + SemMiseBackend,
		"config set --file " + config + " tools.sem.version " + SemVersion,
		"config set --file " + config + " tools.sem.checksum sha256:" + SemLinuxX64SHA256,
		"config set --file " + config + " tool_alias.inspect " + InspectMiseBackend,
		"config set --file " + config + " tools.inspect.version " + InspectVersion,
		"config set --file " + config + " tools.inspect.checksum sha256:" + InspectLinuxX64SHA256,
		"config set --file " + config + " tool_alias.herdr " + HerdrMiseBackend,
		"config set --file " + config + " tools.herdr " + HerdrVersion,
	}
	contents, err := os.ReadFile(miseLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimSpace(string(contents)), "\n"); !slices.Equal(got, want) {
		t.Fatalf("mise config calls = %#v, want %#v", got, want)
	}
}

func TestRemoveLegacySGToolForTarget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		legacySG   string
		unuseFails string
		wantCode   int
		wantUnuse  bool
	}{
		{name: "absent"},
		{name: "present", legacySG: "1", wantUnuse: true},
		{name: "unuse-fails", legacySG: "1", unuseFails: "1", wantCode: 1, wantUnuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			binDir := filepath.Join(home, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			miseLog := filepath.Join(home, "mise.log")
			fakeMise := `#!/bin/sh
set -eu
if [ "${1:-}" = config ] && [ "${2:-}" = get ]; then
  [ "${LEGACY_SG:-}" = 1 ]
  exit
fi
printf '%s\n' "$*" >>"$MISE_LOG"
if [ "${1:-}" = --yes ] && [ "${2:-}" = unuse ] && [ "${UNUSE_FAILS:-}" = 1 ]; then exit 1; fi
`
			if err := os.WriteFile(filepath.Join(binDir, "mise"), []byte(fakeMise), 0o755); err != nil {
				t.Fatal(err)
			}
			code, _, stderr := runHelper(t, `
TARGET_USER=deploy
TARGET_HOME="$FAKE_HOME"
run_as_target() { HOME="$FAKE_HOME" PATH="$FAKE_BIN:$PATH" MISE_LOG="$MISE_LOG" LEGACY_SG="$LEGACY_SG" UNUSE_FAILS="$UNUSE_FAILS" bash -c "$1"; }
remove_legacy_sg_tool_for_target
`, "FAKE_HOME="+home, "FAKE_BIN="+binDir, "MISE_LOG="+miseLog, "LEGACY_SG="+tc.legacySG, "UNUSE_FAILS="+tc.unuseFails)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, tc.wantCode, stderr)
			}
			contents, err := os.ReadFile(miseLog)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			config := filepath.Join(home, ".config", "mise", "config.toml")
			want := "--yes unuse --path " + config + " sg"
			if strings.Contains(string(contents), want) != tc.wantUnuse {
				t.Fatalf("mise log = %q, want unuse=%v", contents, tc.wantUnuse)
			}
		})
	}
}

func TestBootstrapPackageSetUpgradesInstalledPackages(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		`bootstrap packages apply --yes`,
		`bootstrap packages upgrade --yes`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("system package convergence missing %q", want)
		}
	}
}

// TestInstallUserToolsRepairsOnlyFailedComponents executes the install
// orchestration with stubbed readiness probes and asserts only failed
// components are reinstalled (audit: doctor repair of one stale tool must not
// reinstall the healthy toolchain, and a digest that stays wrong must block
// the Pi integration step).
func TestInstallUserToolsRepairsOnlyFailedComponents(t *testing.T) {
	cases := []struct {
		name           string
		target         string
		stubs          string
		wantCode       int
		wantLog        string
		mustContain    []string
		mustNotContain []string
	}{
		{
			name:           "all-healthy",
			target:         "all",
			wantLog:        "already installed",
			mustNotContain: []string{"mise --yes install", "npm install -g", "integration install"},
		},
		{
			name:           "integration-stale-only",
			target:         "all",
			stubs:          "target_herdr_pi_integration_ready() { return 1; }",
			mustContain:    []string{"herdr integration install pi"},
			mustNotContain: []string{"mise --yes install", "npm install -g"},
		},
		{
			name:           "tmux-stale-only",
			target:         "all",
			stubs:          `target_managed_mise_tool_ready() { [[ $1 != tmux\|* ]]; }`,
			mustContain:    []string{"mise --yes install tmux@" + TmuxVersion},
			mustNotContain: []string{"node@", "--force herdr", "npm install -g", "integration install"},
		},
		{
			name:           "uv-stale-only",
			target:         "all",
			stubs:          `target_managed_mise_tool_ready() { [[ $1 != uv\|* ]]; }`,
			mustContain:    []string{"mise --yes install uv@" + UVVersion},
			mustNotContain: []string{"node@", "rust@", "--force herdr", "npm install -g", "integration install"},
		},
		{
			name:           "rust-stale-only",
			target:         "all",
			stubs:          `target_managed_mise_tool_ready() { [[ $1 != rust\|* ]]; }`,
			mustContain:    []string{"mise --yes install --force rust@" + RustVersion},
			mustNotContain: []string{"node@", "uv@", "--force herdr", "npm install -g", "integration install"},
		},
		{
			name:           "ast-grep-stale-only",
			target:         "all",
			stubs:          `target_managed_mise_tool_ready() { [[ $1 != ast-grep\|* ]]; }`,
			mustContain:    []string{"mise --yes install --force ast-grep@" + AstGrepVersion},
			mustNotContain: []string{"sem@", "inspect@", "--force herdr", "npm install -g", "integration install"},
		},
		{
			name:           "sem-stale-only",
			target:         "all",
			stubs:          `target_managed_mise_tool_ready() { [[ $1 != sem\|* ]]; }`,
			mustContain:    []string{"mise --yes install --force sem@" + SemVersion},
			mustNotContain: []string{"ast-grep@", "inspect@", "--force herdr", "npm install -g", "integration install"},
		},
		{
			name:           "inspect-stale-only",
			target:         "all",
			stubs:          `target_managed_mise_tool_ready() { [[ $1 != inspect\|* ]]; }`,
			mustContain:    []string{"mise --yes install --force inspect@" + InspectVersion},
			mustNotContain: []string{"ast-grep@", "sem@", "--force herdr", "npm install -g", "integration install"},
		},
		{
			name:           "git-gh-stale-only",
			target:         "git",
			stubs:          `target_managed_mise_tool_ready() { [[ $1 != gh\|* ]]; }`,
			mustContain:    []string{"mise --yes install gh@" + GitHubCLIVersion},
			mustNotContain: []string{"node@", "npm install -g", "herdr@"},
		},
		{
			name:           "pi-stale-only",
			target:         "pi",
			stubs:          "target_pi_ready() { return 1; }",
			mustContain:    []string{"npm install -g " + PiToolName + "@" + PiVersion},
			mustNotContain: []string{"mise --yes install node@"},
		},
		{
			name:           "herdr-digest-unfixable",
			target:         "all",
			stubs:          "target_herdr_ready() { return 1; }",
			wantCode:       1,
			mustContain:    []string{"--force herdr@" + HerdrVersion},
			mustNotContain: []string{"integration install pi"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := filepath.Join(t.TempDir(), "capture")
			snippet := `
BOOTSTRAP_TARGET=` + tc.target + `
TARGET_USER=deploy
TARGET_HOME=/home/deploy
TARGET_GID=1000
ensure_mise_shell_activation() { :; }
repair_mise_config_for_user() { :; }
configure_user_tools_for_target() { :; }
run_as_target() { printf '%s\n' "$1" >>"$CAPTURE"; }
target_node_ready() { return 0; }
target_pi_ready() { return 0; }
target_herdr_ready() { return 0; }
target_herdr_pi_integration_ready() { return 0; }
` + tc.stubs + `
install_user_tools_for_target ` + tc.target + `
`
			env := append(manifestEnv(), "CAPTURE="+capture)
			code, _, stderr := runHelper(t, snippet, env...)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, tc.wantCode, stderr)
			}
			if tc.wantLog != "" && !strings.Contains(stderr, tc.wantLog) {
				t.Fatalf("stderr missing %q: %s", tc.wantLog, stderr)
			}
			_, captured, _ := runHelper(t, `cat "$CAPTURE"`, "CAPTURE="+capture)
			for _, want := range tc.mustContain {
				if !strings.Contains(captured, want) {
					t.Fatalf("captured commands missing %q:\n%s", want, captured)
				}
			}
			for _, unwanted := range tc.mustNotContain {
				if strings.Contains(captured, unwanted) {
					t.Fatalf("captured commands contain %q:\n%s", unwanted, captured)
				}
			}
		})
	}
}

// TestShellTargetHerdrReadyVerifiesDigestBeforeExecution drives the real
// target_herdr_ready against a fake mise/herdr pair and proves a wrong SHA-256
// blocks herdr execution entirely (audit: the digest gate must precede any
// execution, not just the reported result).
func TestShellTargetHerdrReadyVerifiesDigestBeforeExecution(t *testing.T) {
	requireBash(t)
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum not available on this host")
	}
	cases := []struct {
		name         string
		herdrVersion string
		realDigest   bool
		wantOK       bool
		wantExecuted bool
	}{
		{"valid-binary", HerdrVersion, true, true, true},
		{"wrong-digest", HerdrVersion, false, false, false},
		{"wrong-version", "9.9.9", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fakeHome := filepath.Join(dir, "home")
			fakeBin := filepath.Join(dir, "bin")
			miseBin := filepath.Join(fakeHome, ".local", "bin")
			for _, d := range []string{fakeBin, miseBin} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			herdrScript := "#!/bin/sh\n" +
				"printf 'executed %s\\n' \"${1:-}\" >>\"$HERDR_LOG\"\n" +
				"if [ \"${1:-}\" = --version ]; then printf 'herdr " + tc.herdrVersion + "\\n'; fi\n"
			digest := sha256.Sum256([]byte(herdrScript))
			if err := os.WriteFile(filepath.Join(fakeBin, "herdr"), []byte(herdrScript), 0o755); err != nil {
				t.Fatal(err)
			}
			miseScript := "#!/bin/sh\n" +
				"if [ \"${1:-}\" = exec ]; then shift; if [ \"${1:-}\" = -- ]; then shift; fi; fi\n" +
				"exec \"$@\"\n"
			if err := os.WriteFile(filepath.Join(miseBin, "mise"), []byte(miseScript), 0o755); err != nil {
				t.Fatal(err)
			}
			sha := strings.Repeat("0", 64)
			if tc.realDigest {
				sha = hex.EncodeToString(digest[:])
			}
			// Duplicate env keys resolve to the last value (os/exec), so the
			// override simply appends after manifestEnv().
			env := append(manifestEnv(),
				"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64="+sha,
				"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64="+sha,
				"FAKE_HOME="+fakeHome,
				"FAKE_BIN="+fakeBin,
				"HERDR_LOG="+filepath.Join(dir, "herdr.log"))
			snippet := `
TARGET_USER=deploy
export HERDR_LOG
run_as_target() { HOME="$FAKE_HOME" PATH="$FAKE_BIN:$PATH" bash -c "$1"; }
status=0
target_herdr_ready || status=$?
printf 'LOG=['
cat "$HERDR_LOG" 2>/dev/null
printf ']'
exit "$status"
`
			code, stdout, stderr := runHelper(t, snippet, env...)
			if tc.wantOK && code != 0 {
				t.Fatalf("want ready, got exit %d (stderr: %s)", code, stderr)
			}
			if !tc.wantOK && code == 0 {
				t.Fatal("want not-ready, got exit 0")
			}
			executed := strings.Contains(stdout, "executed --version")
			if executed != tc.wantExecuted {
				t.Fatalf("herdr executed = %v, want %v (stdout: %s)", executed, tc.wantExecuted, stdout)
			}
		})
	}
}

func TestShellTargetNewManagedVersionProbeMatrix(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		output string
		status string
		wantOK bool
	}{
		{name: "ast-grep-exact", key: "ast-grep", output: "ast-grep " + AstGrepVersion, wantOK: true},
		{name: "ast-grep-metadata", key: "ast-grep", output: "ast-grep " + AstGrepVersion + " (fixture)", wantOK: true},
		{name: "ast-grep-wrong", key: "ast-grep", output: "ast-grep 0.0.0"},
		{name: "ast-grep-malformed", key: "ast-grep", output: "unknown"},
		{name: "ast-grep-missing", key: "ast-grep", status: "127"},
		{name: "sem-exact", key: "sem", output: "sem " + SemVersion, wantOK: true},
		{name: "sem-wrong", key: "sem", output: "sem 0.0.0"},
		{name: "sem-malformed", key: "sem", output: "unknown"},
		{name: "sem-missing", key: "sem", status: "127"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			miseBin := filepath.Join(home, ".local", "bin")
			toolBin := filepath.Join(home, "tools")
			for _, dir := range []string{miseBin, toolBin} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			miseScript := "#!/bin/sh\nshift\n[ \"${1:-}\" != -- ] || shift\nexec \"$@\"\n"
			if err := os.WriteFile(filepath.Join(miseBin, "mise"), []byte(miseScript), 0o755); err != nil {
				t.Fatal(err)
			}
			toolScript := "#!/bin/sh\n[ \"${FAKE_TOOL_STATUS:-0}\" = 0 ] || exit \"$FAKE_TOOL_STATUS\"\nprintf '%s\\n' \"$FAKE_TOOL_OUTPUT\"\n"
			if err := os.WriteFile(filepath.Join(toolBin, tc.key), []byte(toolScript), 0o755); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := runHelper(t, `
TARGET_USER=deploy
run_as_target() { HOME="$FAKE_HOME" PATH="$FAKE_TOOL_BIN:$PATH" bash -c "$1"; }
uname() { printf 'x86_64'; }
status=0
target_managed_mise_tool_ready "$TESTROW" >/dev/null || status=$?
printf '%s' "$status"
`, append(manifestEnv(),
				"FAKE_HOME="+home,
				"FAKE_TOOL_BIN="+toolBin,
				"FAKE_TOOL_OUTPUT="+tc.output,
				"FAKE_TOOL_STATUS="+tc.status,
				"TESTROW="+managedMiseToolRowForTest(t, tc.key),
			)...)
			if code != 0 {
				t.Fatalf("probe harness failed: %d (stderr: %s)", code, stderr)
			}
			if tc.wantOK && stdout != "0" {
				t.Fatalf("probe status = %q, want success", stdout)
			}
			if !tc.wantOK && stdout == "0" {
				t.Fatal("invalid probe output passed")
			}
		})
	}
}

func TestShellTargetInspectReadyHashesBeforeExecution(t *testing.T) {
	requireBash(t)
	shaPath, err := exec.LookPath("sha256sum")
	if err != nil {
		shaPath, err = exec.LookPath("gsha256sum")
	}
	if err != nil {
		t.Skip("sha256sum-compatible command not available on this host")
	}
	for _, tc := range []struct {
		name         string
		realDigest   bool
		help         string
		omitBinary   bool
		wantOK       bool
		wantExecuted bool
	}{
		{name: "valid-binary", realDigest: true, help: "Entity-level code review", wantOK: true, wantExecuted: true},
		{name: "wrong-digest", help: "Entity-level code review"},
		{name: "missing-binary", realDigest: true, help: "Entity-level code review", omitBinary: true},
		{name: "wrong-help", realDigest: true, help: "unknown command", wantExecuted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fakeHome := filepath.Join(dir, "home")
			fakeBin := filepath.Join(dir, "bin")
			miseBin := filepath.Join(fakeHome, ".local", "bin")
			for _, d := range []string{fakeBin, miseBin} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			inspectScript := "#!/bin/sh\n" +
				"printf 'executed %s\\n' \"${1:-}\" >>\"$INSPECT_LOG\"\n" +
				"printf '%s\\n' \"$INSPECT_HELP\"\n"
			digest := sha256.Sum256([]byte(inspectScript))
			if !tc.omitBinary {
				if err := os.WriteFile(filepath.Join(fakeBin, "inspect"), []byte(inspectScript), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			miseScript := "#!/bin/sh\n" +
				"if [ \"${1:-}\" = exec ]; then shift; if [ \"${1:-}\" = -- ]; then shift; fi; fi\n" +
				"exec \"$@\"\n"
			if err := os.WriteFile(filepath.Join(miseBin, "mise"), []byte(miseScript), 0o755); err != nil {
				t.Fatal(err)
			}
			shaScript := "#!/bin/sh\nexec " + strconv.Quote(shaPath) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(fakeBin, "sha256sum"), []byte(shaScript), 0o755); err != nil {
				t.Fatal(err)
			}
			sha := strings.Repeat("0", 64)
			if tc.realDigest {
				sha = hex.EncodeToString(digest[:])
			}
			env := append(manifestEnv(),
				"SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_X64="+sha,
				"SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_ARM64="+sha,
				"FAKE_HOME="+fakeHome,
				"FAKE_BIN="+fakeBin,
				"INSPECT_HELP="+tc.help,
				"INSPECT_LOG="+filepath.Join(dir, "inspect.log"),
				"TESTROW="+managedMiseToolRowForTest(t, "inspect"))
			snippet := `
TARGET_USER=deploy
export INSPECT_HELP INSPECT_LOG
uname() { printf 'x86_64'; }
run_as_target() { HOME="$FAKE_HOME" PATH="$FAKE_BIN:/usr/bin:/bin" bash -c "$1"; }
ready=0
target_managed_mise_tool_ready "$TESTROW" >/dev/null || ready=$?
verified=0
verify_managed_mise_tool inspect >/dev/null || verified=$?
printf '%s %s LOG=[' "$ready" "$verified"
if [[ -f $INSPECT_LOG ]]; then cat "$INSPECT_LOG"; fi
printf ']'
`
			code, stdout, stderr := runHelper(t, snippet, env...)
			if code != 0 {
				t.Fatalf("inspect harness failed: %d (stderr: %s)", code, stderr)
			}
			passed := strings.HasPrefix(stdout, "0 0 LOG=[")
			if passed != tc.wantOK {
				t.Fatalf("inspect passed = %v, want %v (stdout: %s)", passed, tc.wantOK, stdout)
			}
			executed := strings.Contains(stdout, "executed --help")
			if executed != tc.wantExecuted {
				t.Fatalf("inspect executed = %v, want %v (stdout: %s)", executed, tc.wantExecuted, stdout)
			}
		})
	}
}
