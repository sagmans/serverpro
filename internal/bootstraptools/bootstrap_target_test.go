package bootstraptools

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestInstallScriptTargetSelectsSingleFamily(t *testing.T) {
	script, err := InstallScriptForUserTarget("deploy", TargetGit)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SERVERPRO_BOOTSTRAP_TARGET='git'", "git) verify_git", "install_git"} {
		if !contains(script, want) {
			t.Fatalf("git target script missing %q", want)
		}
	}
	if _, err := InstallScriptForUserTarget("deploy", Target("bad")); err == nil || !contains(err.Error(), "unsupported bootstrap target") {
		t.Fatalf("expected invalid target error, got %v", err)
	}
}

func TestGitTargetIncludesAccountAccessPrerequisites(t *testing.T) {
	script, err := InstallScriptForUserTarget("deploy", TargetGit)
	if err != nil {
		t.Fatal(err)
	}
	runStart := strings.Index(script, "run_bootstrap_target() {")
	verifyStart := strings.Index(script, "verify_installation() {")
	mainStart := strings.Index(script, "main() {")
	if runStart < 0 || verifyStart <= runStart || mainStart <= verifyStart {
		t.Fatal("missing bootstrap target boundaries")
	}
	runBody := script[runStart:verifyStart]
	verifyBody := script[verifyStart:mainStart]
	for _, want := range []string{"git)\n      install_mise\n      install_git\n      install_user_tools_for_target git", "git) verify_git; verify_mise; verify_managed_mise_tool gh ;;"} {
		body := runBody
		if strings.HasPrefix(want, "git) verify") {
			body = verifyBody
		}
		if !strings.Contains(body, want) {
			t.Fatalf("git target missing prerequisite sequence %q", want)
		}
	}
}

func TestParseTargetDefaultsAndRejectsUnknown(t *testing.T) {
	got, err := ParseTarget("")
	if err != nil {
		t.Fatal(err)
	}
	if got != TargetAll {
		t.Fatalf("empty target = %q", got)
	}
	got, err = ParseTarget(" Git ")
	if err != nil {
		t.Fatal(err)
	}
	if got != TargetGit {
		t.Fatalf("git target = %q", got)
	}
	if _, err := ParseTarget("gh"); err == nil || !contains(err.Error(), "unsupported bootstrap target") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestTargetIncludesGitOnlyForGitFamily(t *testing.T) {
	for _, target := range []Target{TargetAll, TargetGit} {
		if !target.IncludesGit() {
			t.Fatalf("%s should include git deploy follow-up", target)
		}
	}
	for _, target := range []Target{TargetDocker, TargetMise, TargetNode, TargetPi} {
		if target.IncludesGit() {
			t.Fatalf("%s should not include git deploy follow-up", target)
		}
	}
}

func TestDefaultToolsetDescriptionNamesPinnedTools(t *testing.T) {
	description := DefaultToolsetDescription()
	for _, want := range []string{"Git/OpenSSH", "Docker/Compose", "mise", "Node " + NodeVersion, "npm", "Pi " + PiVersion, "uv " + UVVersion, "Rust " + RustVersion, "tmux " + TmuxVersion, "Herdr " + HerdrVersion, "gh " + GitHubCLIVersion, "rg " + RipgrepVersion, "fd " + FdVersion, "ast-grep " + AstGrepVersion, "sem " + SemVersion, "inspect " + InspectVersion, "htop"} {
		if !contains(description, want) {
			t.Fatalf("description missing %q: %s", want, description)
		}
	}
}

func TestManagedAstGrepContractOmitsDeprecatedSGIdentity(t *testing.T) {
	contract := DefaultToolsetDescription() + "\n" + InstallScriptForUser("deploy")
	for _, retired := range []string{"sg/ast-grep", "SERVERPRO_BOOTSTRAP_SG_", "tool_alias.sg", "tools.sg.", "sg --version"} {
		if strings.Contains(contract, retired) {
			t.Fatalf("managed ast-grep contract retains deprecated %q", retired)
		}
	}
}

func TestManagedVersionManifestPinsApprovedReleases(t *testing.T) {
	want := map[string]string{
		"node":     "24.19.0",
		"pi":       "0.84.1",
		"uv":       "0.12.3",
		"rust":     "1.97.1",
		"tmux":     "3.7b",
		"gh":       "2.97.0",
		"rg":       "15.2.0",
		"fd":       "10.4.2",
		"ast-grep": "0.45.1",
		"sem":      "0.21.0",
		"inspect":  "0.1.1",
	}
	got := map[string]string{
		"node":     NodeVersion,
		"pi":       PiVersion,
		"uv":       UVVersion,
		"rust":     RustVersion,
		"tmux":     TmuxVersion,
		"gh":       GitHubCLIVersion,
		"rg":       RipgrepVersion,
		"fd":       FdVersion,
		"ast-grep": AstGrepVersion,
		"sem":      SemVersion,
		"inspect":  InspectVersion,
	}
	for tool, version := range want {
		if got[tool] != version {
			t.Fatalf("%s version = %q, want %q", tool, got[tool], version)
		}
	}
	for _, pin := range []string{"SERVERPRO_BOOTSTRAP_UV_VERSION='0.12.3'", "SERVERPRO_BOOTSTRAP_RUST_VERSION='1.97.1'", "SERVERPRO_BOOTSTRAP_AST_GREP_VERSION='0.45.1'", "SERVERPRO_BOOTSTRAP_SEM_VERSION='0.21.0'", "SERVERPRO_BOOTSTRAP_INSPECT_VERSION='0.1.1'"} {
		if !contains(InstallScriptForUser("deploy"), pin) {
			t.Fatalf("managed version manifest missing %q", pin)
		}
	}
}

func TestManagedReleaseChecksumsPinApprovedAssets(t *testing.T) {
	want := map[string][2]string{
		"ast-grep": {"76fb6555be6734fb5057dba8d2fb756430f374bb9e1af694cf1ce00e13238d63", "9ee7ec49aada3dc05135d21977af089a33fc3154ada25bab102daca90b5098f2"},
		"sem":      {"4a06f019552add37b4b0693309daaf529eae7f291217d20c291294c790b16b4b", "0480663055d3d7c386dabee6e57766205984ac151bd691540bde0b3be64af27b"},
		"inspect":  {"99cf4ea2a2a1048d8e9369a6a5a11e5f84ee3f3c706e0bde072f9b2bd44e96ba", "2327c1de10ecf40e5199c15fdc4c4b3c173735640294e779c635f4c15771e4f6"},
	}
	got := map[string][2]string{
		"ast-grep": {AstGrepLinuxX64SHA256, AstGrepLinuxArm64SHA256},
		"sem":      {SemLinuxX64SHA256, SemLinuxArm64SHA256},
		"inspect":  {InspectLinuxX64SHA256, InspectLinuxArm64SHA256},
	}
	for tool, digests := range want {
		if got[tool] != digests {
			t.Fatalf("%s digests = %q, want %q", tool, got[tool], digests)
		}
	}
}

func TestManagedMiseSpecificationDrivesManifestAndDoctorChecks(t *testing.T) {
	manifestRows := strings.Split(strings.TrimSpace(managedMiseManifest()), "\n")
	if len(manifestRows) != len(managedMiseTools) {
		t.Fatalf("manifest rows = %d, want %d", len(manifestRows), len(managedMiseTools))
	}
	pairs := make(map[string]string)
	for _, pair := range manifestEnvPairs() {
		pairs[pair[0]] = pair[1]
	}
	checks := Checks("deploy")
	for i, tool := range managedMiseTools {
		wantRow := strings.Join([]string{
			tool.key,
			tool.versionEnv,
			emptyManifestField(tool.aliasKey),
			emptyManifestField(tool.backendEnv),
			tool.versionKey,
			emptyManifestField(tool.profileKey),
			emptyManifestField(tool.profileEnv),
			emptyManifestField(tool.checksumKey),
			emptyManifestField(tool.checksumX64Env),
			emptyManifestField(tool.checksumArm64Env),
			strconv.FormatBool(tool.forceRepair),
			string(tool.probe),
		}, "|")
		if manifestRows[i] != wantRow {
			t.Fatalf("manifest row %d = %q, want %q", i, manifestRows[i], wantRow)
		}
		if pairs[tool.versionEnv] != tool.version {
			t.Fatalf("%s env = %q, want %q", tool.key, pairs[tool.versionEnv], tool.version)
		}
		wantCommand := userHomeCommand("deploy", managedMiseProbeCommand(tool))
		if !slices.Contains(checks, (Check{Name: tool.checkName(), Command: wantCommand})) {
			t.Fatalf("missing canonical check %q", tool.checkName())
		}
	}
}

func TestSystemPackagesForTarget(t *testing.T) {
	assertStrings(t, SystemPackagesForTarget(TargetGit), []string{"apt:git", "apt:openssh-client"})
	assertStrings(t, SystemPackagesForTarget(TargetDocker), []string{"apt:docker-ce", "apt:docker-ce-cli", "apt:containerd.io", "apt:docker-buildx-plugin", "apt:docker-compose-plugin"})
	assertStrings(t, SystemPackagesForTarget(TargetAll), []string{"apt:git", "apt:openssh-client", "apt:docker-ce", "apt:docker-ce-cli", "apt:containerd.io", "apt:docker-buildx-plugin", "apt:docker-compose-plugin", "apt:htop"})
	assertStrings(t, SystemPackagesForTarget(TargetNode), nil)
	assertStrings(t, SystemPackagesForTarget(TargetMise), nil)
	assertStrings(t, SystemPackagesForTarget(TargetPi), nil)
}

// TestInstallScriptStagesOnlyFailedComponents pins the component-idempotent
// repair contract: every managed tool is probed and only failures are staged,
// so doctor repair never reinstalls healthy tools.
func TestInstallScriptStagesOnlyFailedComponents(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		`while IFS= read -r row; do`,
		`target_managed_mise_tool_ready "${row}"`,
		`mise_installs+=("${key}@${version}")`,
		`force_mise_installs+=("${key}@${version}")`,
		`mise --yes install ${mise_installs[*]}`,
		`mise --yes install --force ${force_mise_installs[*]}`,
		`target_pi_ready "${node_version}" "${pi_version}" || install_pi=1`,
		`target_herdr_ready || install_herdr=1`,
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing component staging marker %q", want)
		}
	}
}

// TestInstallScriptValidatesHerdrArchBeforeMutation pins the audit fix: the
// unsupported-architecture failure must happen inside validate_bootstrap_env,
// which main runs before any install function can mutate the host.
func TestInstallScriptValidatesHerdrArchBeforeMutation(t *testing.T) {
	script := InstallScriptForUser("deploy")
	start := strings.Index(script, "validate_bootstrap_env() {")
	end := strings.Index(script, "resolve_target_user() {")
	if start < 0 || end <= start {
		t.Fatal("missing validate_bootstrap_env boundary")
	}
	body := script[start:end]
	if !contains(body, `bootstrap_herdr_sha256_for_arch "$(uname -m)" >/dev/null`) {
		t.Fatalf("validate_bootstrap_env missing pre-mutation Herdr arch gate: %s", body)
	}
}

// TestInstallScriptRegistersVerifiedMiseTemps pins parent-shell temp cleanup:
// fetch_verified_mise_binary runs under command substitution, so its
// register_tmp calls would be lost; callers must register the published path.
func TestInstallScriptRegistersVerifiedMiseTemps(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		`register_tmp "${verified_root_mise}"`,
		`register_tmp "${verified_user_mise}"`,
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing temp registration %q", want)
		}
	}
}

func TestInstallScriptValidatesManagedReleaseArchBeforeMutation(t *testing.T) {
	script := InstallScriptForUser("deploy")
	start := strings.Index(script, "validate_bootstrap_env() {")
	end := strings.Index(script, "resolve_target_user() {")
	if start < 0 || end <= start {
		t.Fatal("missing validate_bootstrap_env boundary")
	}
	body := script[start:end]
	if !contains(body, `managed_mise_tool_sha256_for_arch "${row}" "$(uname -m)" >/dev/null`) {
		t.Fatalf("validate_bootstrap_env missing managed release arch gate: %s", body)
	}
}

func TestInstallScriptConfiguresCuratedManagedTools(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		`configure_managed_mise_tool() {`,
		`configure_mise_value_for_target "${alias_key}" "${backend}"`,
		`configure_mise_value_for_target "${version_key}" "${version}"`,
		`configure_mise_value_for_target "${profile_key}" "${profile}"`,
		`configure_mise_value_for_target "${checksum_key}" "sha256:${checksum}"`,
		`done <<<"$(managed_mise_tool_rows)"`,
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing curated uv/Rust config %q", want)
		}
	}
}

func TestPiManifestPinsRequiredVersion(t *testing.T) {
	if PiVersion != "0.84.1" {
		t.Fatalf("pi version = %q, want 0.84.1", PiVersion)
	}
}

func TestMiseReleaseManifestPinsRequiredVersion(t *testing.T) {
	if MinimumMiseVersion != "2026.8.3" {
		t.Fatalf("mise version = %q, want 2026.8.3", MinimumMiseVersion)
	}
	want := map[string]string{
		"linux-x64":   "8aaf21cc4b36681e90a96e9cdf13e5d7511e9773733f741b1a5f7756ba53b5fc",
		"linux-arm64": "8d0c6142607d814279de0e06f53c9e896b5d267bbced9ee6e2d9e1547fccca8f",
		"linux-armv7": "0b9f93b634e01c37b982e687915749c01265b9a084ce115e6a2b1b9c95c4e9d3",
	}
	got := map[string]string{
		"linux-x64":   MiseLinuxX64TarGzSHA256,
		"linux-arm64": MiseLinuxArm64TarGzSHA256,
		"linux-armv7": MiseLinuxArmv7TarGzSHA256,
	}
	for platform, digest := range want {
		if got[platform] != digest {
			t.Fatalf("mise %s digest = %q, want %q", platform, got[platform], digest)
		}
	}
}

func TestInstallScriptExportsPinnedHerdrManifest(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		"SERVERPRO_BOOTSTRAP_HERDR_VERSION='0.8.0'",
		"SERVERPRO_BOOTSTRAP_HERDR_BACKEND='github:herdrdev/herdr'",
		"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64='b872ea7e40fa2cb17e857ac9b62b1bf26db7b403c622f5d2f3f5b35f6e9acd28'",
		"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64='f647ac66468d9efbc642fe534fb284468f0aea60641606fc008dfc0d82a3ca87'",
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing pinned Herdr manifest %q", want)
		}
	}
}

func TestInstallScriptInstallsPinnedHerdrAndPiIntegration(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		`configure_mise_value_for_target tool_alias.herdr "$(bootstrap_herdr_backend)"`,
		`configure_mise_value_for_target tools.herdr "$(bootstrap_herdr_version)"`,
		`target_herdr_ready || install_herdr=1`,
		`mise --yes install --force herdr@${herdr_version}`,
		`if ! target_herdr_pi_integration_ready; then`,
		`sha256sum \"\${herdr_bin}\"`,
		`Herdr integrity verification failed before Pi integration`,
		`install -d -m 0700 "$HOME/.pi/agent"`,
		`herdr integration install pi`,
		`pi: current (v6)`,
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing pinned Herdr marker %q", want)
		}
	}
	for _, forbidden := range []string{"https://herdr.dev/install.sh", "herdr update", "herdr@latest"} {
		if contains(script, forbidden) {
			t.Fatalf("install script contains forbidden Herdr marker %q", forbidden)
		}
	}
}

func TestInstallScriptInstallsPiWithPinnedNodeNpm(t *testing.T) {
	script, err := InstallScriptForUserTarget("deploy", TargetPi)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`mise_installs+=("node@${node_version}")`,
		`mise --yes install ${mise_installs[*]}`,
		`export MISE_EXPERIMENTAL=1 MISE_YES=1 MISE_NPM_PACKAGE_MANAGER=npm npm_config_ignore_scripts=true NPM_CONFIG_IGNORE_SCRIPTS=true`,
		`\"\$HOME/.local/bin/mise\" exec -- npm install -g ${pi_tool}@${pi_version}`,
		`configure_managed_mise_tool "$(managed_mise_tool_row node)"`,
		`expected_pi=\"\$HOME/.local/share/mise/installs/node/${node_version}/bin/pi\"`,
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing pinned node npm marker %q", want)
		}
	}
	for _, forbidden := range []string{
		`mise --yes install node@${node_version} ${pi_tool}@${pi_version}`,
		`\"tools.${pi_tool}\"`,
		"mise --yes bootstrap --only tools",
	} {
		if contains(script, forbidden) {
			t.Fatalf("install script contains stale mise npm-backend marker %q", forbidden)
		}
	}
}

func TestChecksUsePinnedToolsAndTargetUser(t *testing.T) {
	checks := Checks("deploy")
	if len(checks) != 20 {
		t.Fatalf("expected 20 checks, got %d", len(checks))
	}
	assertCheck(t, checks, "git", "git --version")
	assertCheck(t, checks, "managed package updates", "apt-get -s")
	assertCheck(t, checks, "managed package updates", "docker-compose-plugin")
	assertCheck(t, checks, "git", "ssh -V")
	assertCheck(t, checks, "docker engine", "systemctl is-active docker")
	assertCheck(t, checks, "docker compose", "docker compose version")
	assertCheck(t, checks, "htop", "htop --version")
	assertCheck(t, checks, "mise", `test -x "$HOME/.local/bin/mise"`)
	assertCheck(t, checks, "mise", MinimumMiseVersion)
	assertCheck(t, checks, "mise", `MISE_EXPERIMENTAL=1 mise bootstrap --help`)
	assertCheck(t, checks, "node "+NodeVersion, `expected_version="`+NodeVersion+`"`)
	assertCheck(t, checks, "node "+NodeVersion, "runuser -u \"$target_user\"")
	assertCheck(t, checks, "node "+NodeVersion, `cd "$HOME"`)
	assertCheck(t, checks, "pi "+PiVersion, "expected pi at")
	assertCheck(t, checks, "pi "+PiVersion, "expected pi "+PiVersion)
	assertCheck(t, checks, "uv "+UVVersion, "uv --version")
	assertCheck(t, checks, "uv "+UVVersion, `expected_version="`+UVVersion+`"`)
	assertCheck(t, checks, "uv "+UVVersion, `case "$uv_version" in "uv $expected_version"|"uv $expected_version "*)`)
	assertCheck(t, checks, "rust "+RustVersion, "rustc --version")
	assertCheck(t, checks, "rust "+RustVersion, "cargo --version")
	assertCheck(t, checks, "rust "+RustVersion, "rustfmt --version")
	assertCheck(t, checks, "rust "+RustVersion, "cargo clippy --version")
	assertCheck(t, checks, "rust "+RustVersion, "rustup component list --installed")
	assertCheck(t, checks, "tmux "+TmuxVersion, `expected_version="`+TmuxVersion+`"`)
	assertCheck(t, checks, "gh "+GitHubCLIVersion, `expected_version="`+GitHubCLIVersion+`"`)
	assertCheck(t, checks, "rg "+RipgrepVersion, `case "$tool_version" in "ripgrep $expected_version"|"ripgrep $expected_version "*)`)
	assertCheck(t, checks, "fd "+FdVersion, `expected_version="`+FdVersion+`"`)
	assertCheck(t, checks, "ast-grep "+AstGrepVersion, `expected_version="`+AstGrepVersion+`"`)
	assertCheck(t, checks, "ast-grep "+AstGrepVersion, "ast-grep --version")
	assertCheck(t, checks, "sem "+SemVersion, `expected_version="`+SemVersion+`"`)
	assertCheck(t, checks, "sem "+SemVersion, "sem --version")
	assertCheck(t, checks, "inspect "+InspectVersion, `expected_version="`+InspectVersion+`"`)
	assertCheck(t, checks, "inspect "+InspectVersion, InspectLinuxX64SHA256)
	assertCheck(t, checks, "inspect "+InspectVersion, InspectLinuxArm64SHA256)
	assertCheck(t, checks, "inspect "+InspectVersion, "inspect --help")
	assertCheck(t, checks, "herdr "+HerdrVersion, "herdr "+HerdrVersion)
	assertCheck(t, checks, "herdr "+HerdrVersion, HerdrLinuxX64SHA256)
	assertCheck(t, checks, "herdr "+HerdrVersion, HerdrLinuxArm64SHA256)
	assertCheck(t, checks, "herdr "+HerdrVersion, "sha256sum")
	assertCheck(t, checks, "herdr pi integration", "pi: current (v6)")
}

func TestInspectCheckHashesBinaryBeforeExecution(t *testing.T) {
	for _, check := range Checks("deploy") {
		if check.Name != "inspect "+InspectVersion {
			continue
		}
		digestIndex := strings.Index(check.Command, "sha256sum")
		helpIndex := strings.Index(check.Command, "inspect --help")
		if digestIndex < 0 || helpIndex < 0 || digestIndex > helpIndex {
			t.Fatalf("inspect check must verify digest before execution: %s", check.Command)
		}
		return
	}
	t.Fatal("missing inspect check")
}

func TestHerdrChecksHashBinaryBeforeExecution(t *testing.T) {
	for _, check := range Checks("deploy") {
		if check.Name != "herdr "+HerdrVersion {
			continue
		}
		digestIndex := strings.Index(check.Command, "sha256sum")
		versionIndex := strings.Index(check.Command, "herdr --version")
		if digestIndex < 0 || versionIndex < 0 || digestIndex > versionIndex {
			t.Fatalf("Herdr check must verify digest before execution: %s", check.Command)
		}
		return
	}
	t.Fatal("missing Herdr check")
}

// TestInstallScriptHashesHerdrBeforeExecution pins the fail-closed order: the
// shared integrity probe hashes the resolved binary before any herdr
// execution, and both readiness and verification go through it.
func TestInstallScriptHashesHerdrBeforeExecution(t *testing.T) {
	script := InstallScriptForUser("deploy")
	start := strings.Index(script, "herdr_integrity_script() {")
	end := strings.Index(script, "target_herdr_ready() {")
	if start < 0 || end <= start {
		t.Fatal("missing herdr_integrity_script boundary")
	}
	body := script[start:end]
	digestIndex := strings.Index(body, "sha256sum")
	versionIndex := strings.Index(body, "herdr --version")
	if digestIndex < 0 || versionIndex < 0 || digestIndex > versionIndex {
		t.Fatal("herdr_integrity_script must verify digest before execution")
	}
	for _, caller := range []string{"target_herdr_ready() {", "verify_herdr() {"} {
		cstart := strings.Index(script, caller)
		if cstart < 0 {
			t.Fatalf("missing %s", caller)
		}
		cbody := script[cstart : cstart+400]
		if !strings.Contains(cbody, "herdr_integrity_script") {
			t.Fatalf("%s must reuse herdr_integrity_script", caller)
		}
	}
}

func TestHerdrIntegrationCheckHashesBeforeExecution(t *testing.T) {
	for _, check := range Checks("deploy") {
		if check.Name != "herdr pi integration" {
			continue
		}
		digestIndex := strings.Index(check.Command, "sha256sum")
		statusIndex := strings.Index(check.Command, "herdr integration status")
		if digestIndex < 0 || statusIndex < 0 || digestIndex > statusIndex {
			t.Fatalf("Herdr integration check must verify digest before execution: %s", check.Command)
		}
		for _, digest := range []string{HerdrLinuxX64SHA256, HerdrLinuxArm64SHA256} {
			if !strings.Contains(check.Command, digest) {
				t.Fatalf("Herdr integration check missing digest %s", digest)
			}
		}
		return
	}
	t.Fatal("missing Herdr Pi integration check")
}

func TestHerdrIntegrationCheckKeepsEvidencePathFree(t *testing.T) {
	checks := Checks("deploy")
	for _, check := range checks {
		if check.Name != "herdr pi integration" {
			continue
		}
		if strings.Count(check.Command, "integration_status") != 2 {
			t.Fatalf("integration check exposes raw status paths: %s", check.Command)
		}
		if strings.Count(check.Command, "pi: current (v6)") != 2 {
			t.Fatalf("integration check missing sanitized evidence: %s", check.Command)
		}
		return
	}
	t.Fatal("missing Herdr Pi integration check")
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func assertCheck(t *testing.T, checks []Check, name, sub string) {
	t.Helper()
	for _, check := range checks {
		if check.Name == name && contains(check.Command, sub) {
			return
		}
	}
	t.Fatalf("missing check %q containing %q: %+v", name, sub, checks)
}
