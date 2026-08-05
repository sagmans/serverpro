package bootstraptools

import (
	"slices"
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
	for _, want := range []string{"Git/OpenSSH", "Docker/Compose", "mise", "Node " + NodeVersion, "npm", "Pi " + PiVersion, "tmux " + TmuxVersion, "Herdr " + HerdrVersion, "gh " + GitHubCLIVersion, "rg " + RipgrepVersion, "fd " + FdVersion, "htop"} {
		if !contains(description, want) {
			t.Fatalf("description missing %q: %s", want, description)
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
		`target_node_ready "${node_version}" || mise_installs+=("node@${node_version}")`,
		`target_tmux_ready "${tmux_version}" || mise_installs+=("tmux@${tmux_version}")`,
		`target_gh_ready "${gh_version}" || mise_installs+=("gh@${gh_version}")`,
		`target_rg_ready "${rg_version}" || mise_installs+=("rg@${rg_version}")`,
		`target_fd_ready "${fd_version}" || mise_installs+=("fd@${fd_version}")`,
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

func TestPiManifestPinsRequiredVersion(t *testing.T) {
	if PiVersion != "0.82.1" {
		t.Fatalf("pi version = %q, want 0.82.1", PiVersion)
	}
}

func TestMiseReleaseManifestPinsRequiredVersion(t *testing.T) {
	if MinimumMiseVersion != "2026.7.12" {
		t.Fatalf("mise version = %q, want 2026.7.12", MinimumMiseVersion)
	}
	want := map[string]string{
		"linux-x64":   "81a05761cb901808bfae3e494e07ec80329eab66a49cd2fa7b8d9cd1ad96683d",
		"linux-arm64": "763f1bccf74f5c34f766a189a4a029a88d44b83f709e28af497ce2aae2704ead",
		"linux-armv7": "f555ba158515e7346e27d058d8dc9e2d20f95eb1fd685f366c73f0b0bfb965b1",
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
		"SERVERPRO_BOOTSTRAP_HERDR_VERSION='0.7.5'",
		"SERVERPRO_BOOTSTRAP_HERDR_BACKEND='github:ogulcancelik/herdr'",
		"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64='3dc83288073e4c2d3c679a30e7be97bcca9141c6fd17dbbb9219142e95c59253'",
		"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64='32e763a1499a6b694b1d708e4f062b743be1da9f34fcfa4d212d6db6fe09a8b9'",
	} {
		if !contains(script, want) {
			t.Fatalf("install script missing pinned Herdr manifest %q", want)
		}
	}
}

func TestInstallScriptInstallsPinnedHerdrAndPiIntegration(t *testing.T) {
	script := InstallScriptForUser("deploy")
	for _, want := range []string{
		`mise config set --file \"\${config}\" tool_alias.herdr \"${herdr_backend}\"`,
		`mise config set --file \"\${config}\" tools.herdr \"${herdr_version}\"`,
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
		`mise config set --file \"\${config}\" tools.node \"${node_version}\"`,
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
	if len(checks) != 14 {
		t.Fatalf("expected 14 checks, got %d", len(checks))
	}
	assertCheck(t, checks, "git", "git --version")
	assertCheck(t, checks, "git", "ssh -V")
	assertCheck(t, checks, "docker engine", "systemctl is-active docker")
	assertCheck(t, checks, "docker compose", "docker compose version")
	assertCheck(t, checks, "htop", "htop --version")
	assertCheck(t, checks, "mise", `test -x "$HOME/.local/bin/mise"`)
	assertCheck(t, checks, "mise", MinimumMiseVersion)
	assertCheck(t, checks, "mise", `MISE_EXPERIMENTAL=1 mise bootstrap --help`)
	assertCheck(t, checks, "node "+NodeVersion, "v"+NodeVersion)
	assertCheck(t, checks, "node "+NodeVersion, "runuser -u \"$target_user\"")
	assertCheck(t, checks, "node "+NodeVersion, `cd "$HOME"`)
	assertCheck(t, checks, "pi "+PiVersion, "expected pi at")
	assertCheck(t, checks, "pi "+PiVersion, "expected pi "+PiVersion)
	assertCheck(t, checks, "tmux "+TmuxVersion, "tmux "+TmuxVersion)
	assertCheck(t, checks, "gh "+GitHubCLIVersion, GitHubCLIVersion)
	assertCheck(t, checks, "rg "+RipgrepVersion, `case "$rg_version" in "ripgrep `+RipgrepVersion+`"*)`)
	assertCheck(t, checks, "fd "+FdVersion, "fd "+FdVersion)
	assertCheck(t, checks, "herdr "+HerdrVersion, "herdr "+HerdrVersion)
	assertCheck(t, checks, "herdr "+HerdrVersion, HerdrLinuxX64SHA256)
	assertCheck(t, checks, "herdr "+HerdrVersion, HerdrLinuxArm64SHA256)
	assertCheck(t, checks, "herdr "+HerdrVersion, "sha256sum")
	assertCheck(t, checks, "herdr pi integration", "pi: current (v6)")
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
