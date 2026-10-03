package bootstraptools

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	MinimumMiseVersion                = "2026.9.18"
	MiseLinuxX64TarGzSHA256           = "4312f8fd72a8d6a869cd2aca7444929e2a0ef6f45d2c6f2866a1eacc5bdc2e84"
	MiseLinuxArm64TarGzSHA256         = "4a06b8cc295390e606b9103a29a3b49a1efba75092b05c8aca56b8367f636b37"
	NodeVersion                       = "24.21.0"
	NPMVersion                        = "11.19.0"
	PiVersion                         = "0.87.1"
	PiBraceExpansionVersion           = "5.0.12"
	PiBraceExpansionIntegrity         = "sha512-YovQ3rzhaLMIrDjNDMkNS01tea93qhEhG5xy8f6+R0l+dw3Ki+5sCoIoI942iuLZTHWogWktgwVDhU09iNEimQ=="
	PiDependencyMinimumReleaseAgeDays = "7"
	UVVersion                         = "0.12.18"
	UVMiseBackend                     = "aqua:astral-sh/uv"
	RustVersion                       = "1.98.1"
	RustMiseBackend                   = "core:rust"
	RustProfile                       = "default"
	TmuxVersion                       = "3.7c"
	GitHubCLIVersion                  = "2.102.0"
	RipgrepVersion                    = "15.2.0"
	FdVersion                         = "10.5.0"
	AstGrepVersion                    = "0.45.3"
	AstGrepMiseBackend                = "github:ast-grep/ast-grep"
	AstGrepLinuxX64SHA256             = "f8ac830881339d1edee6b2652f54798c0f4da5a827f2db38a08ee31117783ce8"
	AstGrepLinuxArm64SHA256           = "b39cfbc58da4b869a88b8a4bc57bd5deb0d24541e704cf7c257da7b53ec81c8f"
	SemVersion                        = "0.25.0"
	SemMiseBackend                    = "github:Ataraxy-Labs/sem"
	SemLinuxX64SHA256                 = "7151f577f84eef16b32ab67bbf4336b1878de1f4d7a087583b0a08ee4e0fb4fd"
	SemLinuxArm64SHA256               = "ce0ab8f8c7bbacde1c8ed87e5a10b4cd10737870597285da8cd62e905ad3bd8c"
	InspectVersion                    = "0.1.1"
	InspectMiseBackend                = "github:Ataraxy-Labs/inspect"
	InspectLinuxX64SHA256             = "99cf4ea2a2a1048d8e9369a6a5a11e5f84ee3f3c706e0bde072f9b2bd44e96ba"
	InspectLinuxArm64SHA256           = "2327c1de10ecf40e5199c15fdc4c4b3c173735640294e779c635f4c15771e4f6"
	HerdrVersion                      = "0.9.1"
	HerdrLinuxX64SHA256               = "2a02fed16beb651ef006e1d43f048f652ca4dc58ad053cd2d44450563d5c54b7"
	HerdrLinuxArm64SHA256             = "f4ccf4de745f2cb9a39a983e9ba3703dad50ec2a58dea83026ceab721bbd8d9e"
	PiToolName                        = "@earendil-works/pi-coding-agent"
	HerdrMiseBackend                  = "github:herdrdev/herdr"
	ManagedPackageCheckName           = "managed package updates"
	AuthenticationBoundary            = "Pi authentication remains operator-owned; full GitHub development access requires a PAT, and serverpro stores gh credentials only on the managed remote host"
)

type Target string

const (
	TargetAll    Target = "all"
	TargetGit    Target = "git"
	TargetDocker Target = "docker"
	TargetMise   Target = "mise"
	TargetNode   Target = "node"
	TargetPi     Target = "pi"
)

var (
	baseSystemPackages   = hostplatform.APTTokens(hostplatform.BasePackageBaselines())
	gitSystemPackages    = hostplatform.APTTokens(hostplatform.GitPackageBaselines())
	dockerSystemPackages = hostplatform.APTTokens(hostplatform.DockerPackageBaselines())
	htopSystemPackages   = hostplatform.APTTokens(hostplatform.HtopPackageBaselines())
)

//go:embed serverpro-bootstrap-tools.sh
var installScript string

func ParseTarget(s string) (Target, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		s = string(TargetAll)
	}
	target := Target(s)
	if !target.Valid() {
		return "", unsupportedTargetError(s)
	}
	return target, nil
}

func (t Target) Valid() bool {
	switch t {
	case TargetAll, TargetGit, TargetDocker, TargetMise, TargetNode, TargetPi:
		return true
	default:
		return false
	}
}

func (t Target) IncludesGit() bool {
	return t == TargetAll || t == TargetGit
}

func SystemPackagesForTarget(target Target) []string {
	switch target {
	case TargetAll:
		return slices.Concat(baseSystemPackages, gitSystemPackages, dockerSystemPackages, htopSystemPackages)
	case TargetGit:
		return slices.Clone(gitSystemPackages)
	case TargetDocker:
		return slices.Clone(dockerSystemPackages)
	default:
		return nil
	}
}

func DefaultToolsetDescription() string {
	gitPackages := hostplatform.GitPackageBaselines()
	dockerPackages := hostplatform.DockerPackageBaselines()
	htopPackage := hostplatform.HtopPackageBaselines()[0]
	return "Git " + gitPackages[0].MinimumVersion + ", OpenSSH " + gitPackages[1].MinimumVersion + ", Docker " + dockerPackages[0].MinimumVersion + ", Docker CLI " + dockerPackages[1].MinimumVersion + ", containerd " + dockerPackages[2].MinimumVersion + ", Buildx " + dockerPackages[3].MinimumVersion + ", Compose " + dockerPackages[4].MinimumVersion + ", mise " + MinimumMiseVersion + "+, Node " + NodeVersion + ", npm " + NPMVersion + ", Pi " + PiVersion + ", uv " + UVVersion + ", Rust " + RustVersion + ", tmux " + TmuxVersion + ", Herdr " + HerdrVersion + ", gh " + GitHubCLIVersion + ", rg " + RipgrepVersion + ", fd " + FdVersion + ", ast-grep " + AstGrepVersion + ", sem " + SemVersion + ", inspect " + InspectVersion + ", and htop " + htopPackage.MinimumVersion
}

func InstallScriptForUser(user string) string {
	script, err := InstallScriptForUserTarget(user, TargetAll)
	if err != nil {
		panic(err)
	}
	return script
}

// manifestEnvPairs returns the pinned bootstrap manifest as ordered name/value
// pairs. It is the single authority consumed by remote script delivery
// (InstallScriptForUserTarget) and the generated manual wrapper
// (WrapperScript), so a pin bump cannot drift between the two paths.
func manifestEnvPairs() [][2]string {
	pairs := [][2]string{
		{"SERVERPRO_BOOTSTRAP_HOST_OS", hostplatform.ManagedHostOS},
		{"SERVERPRO_BOOTSTRAP_HOST_VERSION", hostplatform.ManagedHostVersion},
		{"SERVERPRO_BOOTSTRAP_HOST_CODENAME", hostplatform.ManagedHostCodename},
		{"SERVERPRO_BOOTSTRAP_HOST_ARCHITECTURES", strings.Join(hostplatform.ManagedHostKernelArchitectures(), " ")},
		{"SERVERPRO_BOOTSTRAP_PACKAGE_BASELINES", hostplatform.PackageBaselineManifest(hostplatform.BootstrapPackageBaselines())},
		{"SERVERPRO_BOOTSTRAP_MIN_MISE_VERSION", MinimumMiseVersion},
		{"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_X64", MiseLinuxX64TarGzSHA256},
		{"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARM64", MiseLinuxArm64TarGzSHA256},
	}
	pairs = append(pairs, managedMiseEnvPairs()...)
	return append(pairs,
		[2]string{"SERVERPRO_BOOTSTRAP_NPM_VERSION", NPMVersion},
		[2]string{"SERVERPRO_BOOTSTRAP_PI_VERSION", PiVersion},
		[2]string{"SERVERPRO_BOOTSTRAP_PI_BRACE_EXPANSION_VERSION", PiBraceExpansionVersion},
		[2]string{"SERVERPRO_BOOTSTRAP_PI_BRACE_EXPANSION_INTEGRITY", PiBraceExpansionIntegrity},
		[2]string{"SERVERPRO_BOOTSTRAP_PI_DEPENDENCY_MIN_RELEASE_AGE_DAYS", PiDependencyMinimumReleaseAgeDays},
		[2]string{"SERVERPRO_BOOTSTRAP_PI_BRACE_EXPANSION_PROBE_BASE64", base64.StdEncoding.EncodeToString([]byte(piBraceExpansionProbe))},
		[2]string{"SERVERPRO_BOOTSTRAP_HERDR_VERSION", HerdrVersion},
		[2]string{"SERVERPRO_BOOTSTRAP_HERDR_BACKEND", HerdrMiseBackend},
		[2]string{"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64", HerdrLinuxX64SHA256},
		[2]string{"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64", HerdrLinuxArm64SHA256},
		[2]string{"SERVERPRO_BOOTSTRAP_PI_TOOL", PiToolName},
		[2]string{"SERVERPRO_BOOTSTRAP_BASE_PACKAGES", strings.Join(baseSystemPackages, " ")},
		[2]string{"SERVERPRO_BOOTSTRAP_GIT_PACKAGES", strings.Join(gitSystemPackages, " ")},
		[2]string{"SERVERPRO_BOOTSTRAP_DOCKER_PACKAGES", strings.Join(dockerSystemPackages, " ")},
		[2]string{"SERVERPRO_BOOTSTRAP_HTOP_PACKAGES", strings.Join(htopSystemPackages, " ")},
	)
}

func InstallScriptForUserTarget(user string, target Target) (string, error) {
	if !target.Valid() {
		return "", unsupportedTargetError(string(target))
	}
	env := append([][2]string{
		{"SERVERPRO_BOOTSTRAP_USER", user},
		{"SERVERPRO_BOOTSTRAP_TARGET", string(target)},
	}, manifestEnvPairs()...)
	var b strings.Builder
	exportNames := make([]string, 0, len(env))
	for _, variable := range env {
		writeScriptEnv(&b, variable[0], variable[1])
		exportNames = append(exportNames, variable[0])
	}
	b.WriteString("export ")
	b.WriteString(strings.Join(exportNames, " "))
	b.WriteByte('\n')
	b.WriteString(installScript)
	return b.String(), nil
}

// WrapperScript renders scripts/serverpro-bootstrap-tools.sh: the manual
// entrypoint that seeds the pinned manifest as overridable defaults, then
// delegates to the canonical script. Generated from manifestEnvPairs so the
// wrapper can never drift from remote delivery; regenerate via
// cmd/genbootstrapwrapper.
func WrapperScript() string {
	pairs := manifestEnvPairs()
	var b strings.Builder
	b.WriteString("#!/bin/sh\n# Code generated by go run ./cmd/genbootstrapwrapper from the internal/bootstraptools\n# manifest. DO NOT EDIT: bump tool pins in internal/bootstraptools/bootstrap.go and\n# host/package baselines in internal/hostplatform/platform.go instead.\nset -eu\n\n")
	names := make([]string, 0, len(pairs))
	for _, kv := range pairs {
		fmt.Fprintf(&b, "%s=\"${%s:-%s}\"\n", kv[0], kv[0], kv[1])
		names = append(names, kv[0])
	}
	b.WriteString("export ")
	b.WriteString(strings.Join(names, " "))
	b.WriteString("\n\n")
	b.WriteString(`script_dir=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
canonical=${script_dir}/../internal/bootstraptools/serverpro-bootstrap-tools.sh
if [ ! -f "${canonical}" ]; then
  printf 'missing canonical bootstrap script: %s\n' "${canonical}" >&2
  exit 1
fi
exec sh "${canonical}" "$@"
`)
	return b.String()
}

type Check struct {
	Name    string
	Command string
}

func Checks(user string) []Check {
	checks := []Check{
		{Name: "git", Command: userHomeCommand(user, "command -v git >/dev/null && git --version && command -v ssh >/dev/null && ssh -V 2>&1")},
		{Name: "docker engine", Command: "command -v docker >/dev/null && docker --version && systemctl is-active docker"},
		{Name: "docker compose", Command: "docker compose version"},
		{Name: "htop", Command: "command -v htop >/dev/null && htop --version | head -n1"},
		{Name: ManagedPackageCheckName, Command: managedPackageUpdatesCommand()},
		{Name: "mise", Command: userHomeCommand(user, miseCheckCommand())},
	}
	for _, tool := range managedMiseTools {
		checks = append(checks, Check{Name: tool.checkName(), Command: userHomeCommand(user, managedMiseProbeCommand(tool))})
		if tool.key == "node" {
			checks = append(checks,
				Check{Name: "npm " + NPMVersion, Command: userHomeCommand(user, `actual_npm=$("$HOME/.local/bin/mise" exec -- npm --version); test "$actual_npm" = "`+NPMVersion+`" || { printf 'expected npm `+NPMVersion+`, got %s\n' "$actual_npm" >&2; exit 1; }; printf '%s\n' "$actual_npm"`)},
				Check{Name: "pi " + PiVersion, Command: userHomeCommand(user, `expected_pi="$HOME/.local/share/mise/installs/node/`+NodeVersion+`/bin/pi"; actual_pi=$("$HOME/.local/bin/mise" exec -- sh -c 'command -v pi'); test "$actual_pi" = "$expected_pi" || { printf 'expected pi at %s, got %s\n' "$expected_pi" "$actual_pi" >&2; exit 1; }; `+piBraceExpansionCheckCommand()+`; pi_version=$("$HOME/.local/bin/mise" exec -- pi --version 2>&1) || { status=$?; printf 'pi --version failed (%s): %s\n' "$status" "$pi_version" >&2; exit "$status"; }; test "$pi_version" = "`+PiVersion+`" || { printf 'expected pi `+PiVersion+`, got %s\n' "$pi_version" >&2; exit 1; }; printf '%s\n' "$pi_version"`)},
			)
		}
	}
	herdrVerified := herdrVerifiedCommand()
	return append(checks,
		Check{Name: "herdr " + HerdrVersion, Command: userHomeCommand(user, herdrVerified+`; printf '%s\nsha256 %s\n' "$herdr_version" "$actual_sha"`)},
		Check{Name: "herdr pi integration", Command: userHomeCommand(user, herdrVerified+`; integration_status=$("$HOME/.local/bin/mise" exec -- herdr integration status); printf '%s\n' "$integration_status" | grep -Fq 'pi: current'; printf 'pi: current\n'`)},
	)
}

func ManagedPackageRefreshCommand() string {
	return "DEBIAN_FRONTEND=noninteractive apt-get update"
}

func managedPackageUpdatesCommand() string {
	packages := hostplatform.BootstrapPackageBaselines()
	quoted := make([]string, len(packages))
	var baselineChecks strings.Builder
	baselineChecks.WriteString(`export LC_ALL=C; installed_package_version() { package_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' "$1" 2>/dev/null) || return 1; case "$package_record" in installed'|'*) printf '%s' "${package_record#installed|}" ;; *) return 1 ;; esac; }; `)
	for i, pkg := range packages {
		name := shell.Quote(pkg.Name)
		minimum := shell.Quote(pkg.MinimumVersion)
		quoted[i] = name
		fmt.Fprintf(&baselineChecks, `installed=$(installed_package_version %s) || { printf 'managed package missing: %s\n' >&2; exit 1; }; dpkg --compare-versions "$installed" ge %s || { printf 'managed package below baseline: %s %%s < %s\n' "$installed" >&2; exit 1; }; `, name, pkg.Name, minimum, pkg.Name, pkg.MinimumVersion)
	}
	return baselineChecks.String() + "set -- " + strings.Join(quoted, " ") + `; out=$(apt-get -s -o Debug::NoLocking=1 --no-install-recommends install "$@" 2>&1) || { printf '%s\n' "$out" >&2; exit 1; }; if printf '%s\n' "$out" | grep -q '^Inst '; then printf 'managed package updates available\n' >&2; exit 1; fi; printf 'current\n'`
}

func herdrVerifiedCommand() string {
	return `herdr_bin=$("$HOME/.local/bin/mise" exec -- sh -c 'command -v herdr'); test -f "$herdr_bin" && test -x "$herdr_bin"; case "$(uname -m)" in x86_64) expected_sha=` + HerdrLinuxX64SHA256 + ` ;; aarch64|arm64) expected_sha=` + HerdrLinuxArm64SHA256 + ` ;; *) printf 'unsupported architecture for Herdr release: %s\n' "$(uname -m)" >&2; exit 1 ;; esac; actual_sha=$(sha256sum "$herdr_bin" | awk '{print $1}'); test "$actual_sha" = "$expected_sha" || { printf 'Herdr SHA-256 mismatch: expected %s, got %s\n' "$expected_sha" "$actual_sha" >&2; exit 1; }; herdr_version=$("$HOME/.local/bin/mise" exec -- herdr --version); test "$herdr_version" = "herdr ` + HerdrVersion + `" || { printf 'expected herdr ` + HerdrVersion + `, got %s\n' "$herdr_version" >&2; exit 1; }`
}

func miseCheckCommand() string {
	return `test -x "$HOME/.local/bin/mise"; current=$(mise --version); current=${current#mise }; current=${current%% *}; test "$(printf '%s\n%s\n' "` + MinimumMiseVersion + `" "$current" | sort -V | head -n1)" = "` + MinimumMiseVersion + `"; MISE_EXPERIMENTAL=1 mise bootstrap --help >/dev/null; mise --version`
}

func userHomeCommand(user, command string) string {
	quotedUser := shell.Quote(user)
	return "target_user=" + quotedUser + "; target_home=$(getent passwd \"$target_user\" | cut -d: -f6); test -n \"$target_home\" && runuser -u \"$target_user\" -- env -i HOME=\"$target_home\" USER=\"$target_user\" LOGNAME=\"$target_user\" PATH=\"$target_home/.local/bin:$target_home/.local/share/mise/shims:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\" MISE_EXPERIMENTAL=1 bash -c " + shell.Quote(`set -euo pipefail; cd "$HOME"; `+command)
}

func writeScriptEnv(b *strings.Builder, name, value string) {
	fmt.Fprintf(b, "%s=%s\n", name, shell.Quote(value))
}

func unsupportedTargetError(target string) error {
	return fmt.Errorf("unsupported bootstrap target %q (want all, git, docker, mise, node, or pi)", target)
}
