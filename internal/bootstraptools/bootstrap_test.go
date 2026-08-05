package bootstraptools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptEmbedsCanonicalScript(t *testing.T) {
	canonical, err := os.ReadFile("serverpro-bootstrap-tools.sh")
	if err != nil {
		t.Fatal(err)
	}
	if installScript != string(canonical) {
		t.Fatal("embedded bootstrap install script drifted from canonical script")
	}
}

// TestGeneratedWrapperMatchesCheckedInFile is the drift gate: the manual
// wrapper is generated from the same manifest as remote delivery. Regenerate
// with `make gen-bootstrap-wrapper` after any manifest change.
func TestGeneratedWrapperMatchesCheckedInFile(t *testing.T) {
	checkedIn, err := os.ReadFile("../../scripts/serverpro-bootstrap-tools.sh")
	if err != nil {
		t.Fatal(err)
	}
	if string(checkedIn) != WrapperScript() {
		t.Fatal("scripts/serverpro-bootstrap-tools.sh is stale; regenerate via make gen-bootstrap-wrapper")
	}
}

// TestWrapperScriptDefaultsAndDelegation proves every manifest pin becomes an
// overridable wrapper default and the wrapper delegates to the canonical
// script, so the manual path and remote delivery share one authority.
func TestWrapperScriptDefaultsAndDelegation(t *testing.T) {
	wrapper := WrapperScript()
	for _, kv := range manifestEnvPairs() {
		want := kv[0] + `="${` + kv[0] + `:-` + kv[1] + `}"`
		if !contains(wrapper, want) {
			t.Fatalf("wrapper missing overridable default %q", want)
		}
	}
	for _, want := range []string{
		"DO NOT EDIT",
		`canonical=${script_dir}/../internal/bootstraptools/serverpro-bootstrap-tools.sh`,
		`if [ ! -f "${canonical}" ]; then`,
		`exec sh "${canonical}" "$@"`,
	} {
		if !contains(wrapper, want) {
			t.Fatalf("wrapper missing delegation marker %q", want)
		}
	}
	// The generated wrapper must parse as POSIX sh.
	script := filepath.Join(t.TempDir(), "wrapper.sh")
	if err := os.WriteFile(script, []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("generated wrapper fails sh -n: %v: %s", err, out)
	}
}

func TestManualScriptRunsCanonicalWithoutExecutableBit(t *testing.T) {
	wrapper, err := os.ReadFile("../../scripts/serverpro-bootstrap-tools.sh")
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	canonicalDir := filepath.Join(root, "internal", "bootstraptools")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(canonicalDir, 0755); err != nil {
		t.Fatal(err)
	}
	wrapperPath := filepath.Join(scriptsDir, "serverpro-bootstrap-tools.sh")
	canonicalPath := filepath.Join(canonicalDir, "serverpro-bootstrap-tools.sh")
	if err := os.WriteFile(wrapperPath, wrapper, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte("#!/bin/sh\nprintf 'canonical:%s\\n' \"$1\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("sh", wrapperPath, "ok").CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	if string(out) != "canonical:ok\n" {
		t.Fatalf("unexpected wrapper output: %q", out)
	}
}

func TestManagedPackageRefreshCommand(t *testing.T) {
	const want = "DEBIAN_FRONTEND=noninteractive apt-get update"
	if got := ManagedPackageRefreshCommand(); got != want {
		t.Fatalf("ManagedPackageRefreshCommand() = %q, want %q", got, want)
	}
}

func TestInstallScriptMutatesUserFilesAsTargetUser(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		"TARGET_GID=",
		`TARGET_GID=$(printf '%s' "${user_record}" | cut -d: -f4)`,
		`-z ${TARGET_GID}`,
		`script=$(cat <<'TARGET_SCRIPT'`,
		`run_as_target "${script}"`,
		`chmod 0644 "${bashrc}"`,
		`install -m 0600 /dev/null "${config}"`,
		`chmod 0600 "${backup}"`,
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing %q", want)
		}
	}
	for _, old := range []string{
		`chown "${TARGET_USER}:${TARGET_USER}"`,
		`chown "${TARGET_USER}:${TARGET_GID}"`,
		`-g "${TARGET_USER}"`,
		`-g "${TARGET_GID}"`,
		`install -d -o "${TARGET_USER}"`,
		`install -o "${TARGET_USER}"`,
	} {
		if contains(script, old) {
			t.Fatalf("install script still mutates user-owned paths as root via %q", old)
		}
	}
}

var installScriptRequiredMarkers = []string{
	"SERVERPRO_BOOTSTRAP_USER='deploy'",
	"SERVERPRO_BOOTSTRAP_TARGET='all'",
	"SERVERPRO_BOOTSTRAP_MIN_MISE_VERSION='" + MinimumMiseVersion + "'",
	"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_X64='" + MiseLinuxX64TarGzSHA256 + "'",
	"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARM64='" + MiseLinuxArm64TarGzSHA256 + "'",
	"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARMV7='" + MiseLinuxArmv7TarGzSHA256 + "'",
	"SERVERPRO_BOOTSTRAP_NODE_VERSION='" + NodeVersion + "'",
	"SERVERPRO_BOOTSTRAP_PI_VERSION='" + PiVersion + "'",
	"SERVERPRO_BOOTSTRAP_UV_VERSION='0.12.0'",
	"SERVERPRO_BOOTSTRAP_UV_BACKEND='aqua:astral-sh/uv'",
	"SERVERPRO_BOOTSTRAP_RUST_VERSION='1.97.1'",
	"SERVERPRO_BOOTSTRAP_RUST_BACKEND='core:rust'",
	"SERVERPRO_BOOTSTRAP_RUST_PROFILE='default'",
	"SERVERPRO_BOOTSTRAP_TMUX_VERSION='" + TmuxVersion + "'",
	"SERVERPRO_BOOTSTRAP_GH_VERSION='" + GitHubCLIVersion + "'",
	"SERVERPRO_BOOTSTRAP_RG_VERSION='" + RipgrepVersion + "'",
	"SERVERPRO_BOOTSTRAP_FD_VERSION='" + FdVersion + "'",
	"SERVERPRO_BOOTSTRAP_AST_GREP_VERSION='" + AstGrepVersion + "'",
	"SERVERPRO_BOOTSTRAP_AST_GREP_BACKEND='" + AstGrepMiseBackend + "'",
	"SERVERPRO_BOOTSTRAP_AST_GREP_SHA256_LINUX_X64='" + AstGrepLinuxX64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_AST_GREP_SHA256_LINUX_ARM64='" + AstGrepLinuxArm64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_SEM_VERSION='" + SemVersion + "'",
	"SERVERPRO_BOOTSTRAP_SEM_BACKEND='" + SemMiseBackend + "'",
	"SERVERPRO_BOOTSTRAP_SEM_SHA256_LINUX_X64='" + SemLinuxX64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_SEM_SHA256_LINUX_ARM64='" + SemLinuxArm64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_INSPECT_VERSION='" + InspectVersion + "'",
	"SERVERPRO_BOOTSTRAP_INSPECT_BACKEND='" + InspectMiseBackend + "'",
	"SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_X64='" + InspectLinuxX64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_ARM64='" + InspectLinuxArm64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_HERDR_VERSION='" + HerdrVersion + "'",
	"SERVERPRO_BOOTSTRAP_HERDR_BACKEND='" + HerdrMiseBackend + "'",
	"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64='" + HerdrLinuxX64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64='" + HerdrLinuxArm64SHA256 + "'",
	"SERVERPRO_BOOTSTRAP_PI_TOOL='" + PiToolName + "'",
	"SERVERPRO_BOOTSTRAP_GIT_PACKAGES='apt:git apt:openssh-client'",
	"SERVERPRO_BOOTSTRAP_DOCKER_PACKAGES='apt:docker-ce apt:docker-ce-cli apt:containerd.io apt:docker-buildx-plugin apt:docker-compose-plugin'",
	"SERVERPRO_BOOTSTRAP_HTOP_PACKAGES='apt:htop'",
	"require_root",
	`validate_version_token "${name}" "${value}"`,
	"validate_sha256_token",
	"bootstrap_sha256_env",
	"validate_package_token",
	"validate_user_token",
	"atomic_install_file",
	"ROOT_MISE=/usr/local/bin/mise",
	"fetch_verified_mise_binary",
	"DOCKER_GPG_FINGERPRINT=9DC858229FC7DD38854AE2D88D81803C0EBFCD88",
	"verify_gpg_fingerprint",
	"chmod 0644 /etc/apt/sources.list.d/docker.sources",
	"mise_release_arch",
	`printf '%s  %s\n' "${checksum}" "${filename}"`,
	"sha256sum -c",
	`filename="mise-v${version}-linux-${arch}.tar.gz"`,
	`mise_bin=${ROOT_MISE}`,
	`"${ROOT_MISE}" --version`,
	"refusing symlinked mise config",
	`install -m 0600 /dev/null "${config}"`,
	`chmod 0600 "${config}"`,
	"refusing unsafe bashrc path",
	"refusing symlinked mise config backup source",
	`export MISE_EXPERIMENTAL=1 MISE_YES=1 MISE_NPM_PACKAGE_MANAGER=npm npm_config_ignore_scripts=true NPM_CONFIG_IGNORE_SCRIPTS=true`,
	`BOOTSTRAP_TARGET=${1:-${SERVERPRO_BOOTSTRAP_TARGET:-all}}`,
	`main "$@"`,
	"case \"${BOOTSTRAP_TARGET}\" in",
	"bootstrap packages apply --yes",
	"bootstrap packages upgrade --yes",
	"bootstrap_package_set git",
	"bootstrap_package_set docker",
	"bootstrap_package_set htop",
	"mise --yes install ${mise_installs[*]}",
	`mise_installs+=("node@${node_version}")`,
	`target_managed_mise_tool_ready "${row}"`,
	`mise_installs+=("${key}@${version}")`,
	`force_mise_installs+=("${key}@${version}")`,
	`mise --yes install --force ${force_mise_installs[*]}`,
	"tool_alias.uv",
	"tool_alias.rust",
	"tools.rust.profile",
	"tool_alias.ast-grep",
	"tools.ast-grep.checksum",
	"tool_alias.sem",
	"tools.sem.checksum",
	"tool_alias.inspect",
	"tools.inspect.checksum",
	"managed_mise_probe_script",
	"target_managed_mise_tool_ready",
	"verify_all_user_tools",
	"verify_managed_mise_tool",
	"rustup component list --installed",
	"tool_alias.herdr",
	"herdr@${herdr_version}",
	"bootstrap_herdr_sha256_for_arch",
	"herdr integration install pi",
	"pi: current (v6)",
	`\"\$HOME/.local/bin/mise\" exec -- npm install -g ${pi_tool}@${pi_version}`,
	"docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc",
	"exec bash \"$0\" \"$@\"",
	"mapfile -t network_ids < <(docker network ls -q 2>/dev/null || true)",
	`repair_mise_config_for_user`,
	`backup_timestamp=$(date -u +%Y%m%d%H%M%S)`,
	`backup=${config}.serverpro-bad-${backup_timestamp}`,
	`expected pi %s, got %s`,
	`case "$tool_version" in "ripgrep $expected_version"|"ripgrep $expected_version "*)`,
	`case "$tool_version" in "ast-grep $expected_version"|"ast-grep $expected_version "*)`,
	`test "$tool_version" = "sem $expected_version"`,
	`inspect --help`,
	`\"\$HOME/.local/bin/mise\" exec -- node --version`,
	`PATH="${TARGET_PATH}"`,
	`bash -c 'cd "$HOME" && exec bash -c "$1"' sh "${script}"`,
	`# serverpro-bootstrap-tools: admin user toolchain`,
	`"$HOME/.local/bin/mise" activate bash`,
	`test -x \"\$HOME/.local/bin/mise\"`,
	`test -f \"\${bin}\" && test ! -L \"\${bin}\" && test -O \"\${bin}\"`,
}

var installScriptUniqueVersions = []string{
	MinimumMiseVersion,
	NodeVersion,
	PiVersion,
	"0.12.0",
	"1.97.1",
	TmuxVersion,
	GitHubCLIVersion,
	RipgrepVersion,
	FdVersion,
	AstGrepVersion,
	SemVersion,
	InspectVersion,
	HerdrVersion,
}

var installScriptUniqueChecksums = []string{
	MiseLinuxX64TarGzSHA256,
	MiseLinuxArm64TarGzSHA256,
	MiseLinuxArmv7TarGzSHA256,
	AstGrepLinuxX64SHA256,
	AstGrepLinuxArm64SHA256,
	SemLinuxX64SHA256,
	SemLinuxArm64SHA256,
	InspectLinuxX64SHA256,
	InspectLinuxArm64SHA256,
	HerdrLinuxX64SHA256,
	HerdrLinuxArm64SHA256,
}

var installScriptForbiddenMarkers = []string{
	"npm:@earendil-works/pi-coding-agent",
	"apt_install git openssh-client",
	"apt_install docker-ce",
	"apt_install htop",
	"mise use --global node",
	"bootstrap_mise_bin()",
	"mise_bin=$(require_bootstrap_capable_mise)",
	"mise_bin=$(require_root_bootstrap_mise)",
	`MISE_INSTALL_PATH="${tmp}" sh`,
	"https://mise.run",
	"https://herdr.dev/install.sh",
	"herdr update",
	"herdr@latest",
	"SHASUMS256.txt",
	"mise --yes bootstrap --only tools",
	`chmod 0644 "${config}"`,
	"github_token",
	"GH_TOKEN",
}

func TestInstallScriptExportsManagedManifestAndNoSecrets(t *testing.T) {
	script := InstallScriptForUser("deploy")
	assertContainsAll(t, script, installScriptRequiredMarkers)
	assertAppearsOnce(t, script, installScriptUniqueVersions)
	assertAppearsOnce(t, script, installScriptUniqueChecksums)
	assertOmitsAll(t, script, installScriptForbiddenMarkers)
}

func assertContainsAll(t *testing.T, script string, markers []string) {
	t.Helper()
	for _, marker := range markers {
		if !contains(script, marker) {
			t.Fatalf("install script missing %q", marker)
		}
	}
}

func assertAppearsOnce(t *testing.T, script string, markers []string) {
	t.Helper()
	for _, marker := range markers {
		if count := strings.Count(script, marker); count != 1 {
			t.Fatalf("%s appears %d times, want exactly once", marker, count)
		}
	}
}

func assertOmitsAll(t *testing.T, script string, markers []string) {
	t.Helper()
	for _, marker := range markers {
		if contains(script, marker) {
			t.Fatalf("install script still contains forbidden marker %q", marker)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return sub == ""
}
