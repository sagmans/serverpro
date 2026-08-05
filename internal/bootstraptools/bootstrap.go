package bootstraptools

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"github.com/sagmans/serverpro/internal/shell"
)

const (
	MinimumMiseVersion        = "2026.7.12"
	MiseLinuxX64TarGzSHA256   = "81a05761cb901808bfae3e494e07ec80329eab66a49cd2fa7b8d9cd1ad96683d"
	MiseLinuxArm64TarGzSHA256 = "763f1bccf74f5c34f766a189a4a029a88d44b83f709e28af497ce2aae2704ead"
	MiseLinuxArmv7TarGzSHA256 = "f555ba158515e7346e27d058d8dc9e2d20f95eb1fd685f366c73f0b0bfb965b1"
	NodeVersion               = "24.11.1"
	PiVersion                 = "0.82.1"
	TmuxVersion               = "3.6b"
	GitHubCLIVersion          = "2.95.0"
	RipgrepVersion            = "15.1.0"
	FdVersion                 = "10.4.2"
	HerdrVersion              = "0.7.5"
	HerdrLinuxX64SHA256       = "3dc83288073e4c2d3c679a30e7be97bcca9141c6fd17dbbb9219142e95c59253"
	HerdrLinuxArm64SHA256     = "32e763a1499a6b694b1d708e4f062b743be1da9f34fcfa4d212d6db6fe09a8b9"
	PiToolName                = "@earendil-works/pi-coding-agent"
	HerdrMiseBackend          = "github:ogulcancelik/herdr"
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
	gitSystemPackages    = []string{"apt:git", "apt:openssh-client"}
	dockerSystemPackages = []string{"apt:docker-ce", "apt:docker-ce-cli", "apt:containerd.io", "apt:docker-buildx-plugin", "apt:docker-compose-plugin"}
	htopSystemPackages   = []string{"apt:htop"}
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
		return slices.Concat(gitSystemPackages, dockerSystemPackages, htopSystemPackages)
	case TargetGit:
		return slices.Clone(gitSystemPackages)
	case TargetDocker:
		return slices.Clone(dockerSystemPackages)
	default:
		return nil
	}
}

func DefaultToolsetDescription() string {
	return "Git/OpenSSH, Docker/Compose, mise, Node " + NodeVersion + ", npm, Pi " + PiVersion + ", tmux " + TmuxVersion + ", Herdr " + HerdrVersion + ", gh " + GitHubCLIVersion + ", rg " + RipgrepVersion + ", fd " + FdVersion + ", and htop"
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
	return [][2]string{
		{"SERVERPRO_BOOTSTRAP_MIN_MISE_VERSION", MinimumMiseVersion},
		{"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_X64", MiseLinuxX64TarGzSHA256},
		{"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARM64", MiseLinuxArm64TarGzSHA256},
		{"SERVERPRO_BOOTSTRAP_MISE_SHA256_LINUX_ARMV7", MiseLinuxArmv7TarGzSHA256},
		{"SERVERPRO_BOOTSTRAP_NODE_VERSION", NodeVersion},
		{"SERVERPRO_BOOTSTRAP_PI_VERSION", PiVersion},
		{"SERVERPRO_BOOTSTRAP_TMUX_VERSION", TmuxVersion},
		{"SERVERPRO_BOOTSTRAP_GH_VERSION", GitHubCLIVersion},
		{"SERVERPRO_BOOTSTRAP_RG_VERSION", RipgrepVersion},
		{"SERVERPRO_BOOTSTRAP_FD_VERSION", FdVersion},
		{"SERVERPRO_BOOTSTRAP_HERDR_VERSION", HerdrVersion},
		{"SERVERPRO_BOOTSTRAP_HERDR_BACKEND", HerdrMiseBackend},
		{"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_X64", HerdrLinuxX64SHA256},
		{"SERVERPRO_BOOTSTRAP_HERDR_SHA256_LINUX_ARM64", HerdrLinuxArm64SHA256},
		{"SERVERPRO_BOOTSTRAP_PI_TOOL", PiToolName},
		{"SERVERPRO_BOOTSTRAP_GIT_PACKAGES", strings.Join(gitSystemPackages, " ")},
		{"SERVERPRO_BOOTSTRAP_DOCKER_PACKAGES", strings.Join(dockerSystemPackages, " ")},
		{"SERVERPRO_BOOTSTRAP_HTOP_PACKAGES", strings.Join(htopSystemPackages, " ")},
	}
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
	b.WriteString("#!/bin/sh\n# Code generated by go run ./cmd/genbootstrapwrapper from the internal/bootstraptools\n# manifest. DO NOT EDIT: bump pins in internal/bootstraptools/bootstrap.go instead.\nset -eu\n\n")
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
	herdrVerified := herdrVerifiedCommand()
	return []Check{
		{Name: "git", Command: userHomeCommand(user, "command -v git >/dev/null && git --version && command -v ssh >/dev/null && ssh -V 2>&1")},
		{Name: "docker engine", Command: "command -v docker >/dev/null && docker --version && systemctl is-active docker"},
		{Name: "docker compose", Command: "docker compose version"},
		{Name: "htop", Command: "command -v htop >/dev/null && htop --version | head -n1"},
		{Name: "mise", Command: userHomeCommand(user, miseCheckCommand())},
		{Name: "node " + NodeVersion, Command: userHomeCommand(user, `node_version=$("$HOME/.local/bin/mise" exec -- node --version); test "$node_version" = "v`+NodeVersion+`" && printf '%s\n' "$node_version"`)},
		{Name: "npm", Command: userHomeCommand(user, `"$HOME/.local/bin/mise" exec -- npm --version`)},
		{Name: "pi " + PiVersion, Command: userHomeCommand(user, `expected_pi="$HOME/.local/share/mise/installs/node/`+NodeVersion+`/bin/pi"; actual_pi=$("$HOME/.local/bin/mise" exec -- sh -c 'command -v pi'); test "$actual_pi" = "$expected_pi" || { printf 'expected pi at %s, got %s\n' "$expected_pi" "$actual_pi" >&2; exit 1; }; pi_version=$("$HOME/.local/bin/mise" exec -- pi --version 2>&1) || { status=$?; printf 'pi --version failed (%s): %s\n' "$status" "$pi_version" >&2; exit "$status"; }; test "$pi_version" = "`+PiVersion+`" || { printf 'expected pi `+PiVersion+`, got %s\n' "$pi_version" >&2; exit 1; }; printf '%s\n' "$pi_version"`)},
		{Name: "tmux " + TmuxVersion, Command: userHomeCommand(user, `tmux_version=$("$HOME/.local/bin/mise" exec -- tmux -V); test "$tmux_version" = "tmux `+TmuxVersion+`" && printf '%s\n' "$tmux_version"`)},
		{Name: "gh " + GitHubCLIVersion, Command: userHomeCommand(user, `out=$("$HOME/.local/bin/mise" exec -- gh --version | head -n1); set -- $out; test "$3" = "`+GitHubCLIVersion+`"; printf '%s\n' "$out"`)},
		{Name: "rg " + RipgrepVersion, Command: userHomeCommand(user, `rg_version=$("$HOME/.local/bin/mise" exec -- rg --version | head -n1); case "$rg_version" in "ripgrep `+RipgrepVersion+`"*) ;; *) printf 'expected ripgrep `+RipgrepVersion+`, got %s\n' "$rg_version" >&2; exit 1 ;; esac; printf '%s\n' "$rg_version"`)},
		{Name: "fd " + FdVersion, Command: userHomeCommand(user, `fd_version=$("$HOME/.local/bin/mise" exec -- fd --version); test "$fd_version" = "fd `+FdVersion+`" && printf '%s\n' "$fd_version"`)},
		{Name: "herdr " + HerdrVersion, Command: userHomeCommand(user, herdrVerified+`; printf '%s\nsha256 %s\n' "$herdr_version" "$actual_sha"`)},
		{Name: "herdr pi integration", Command: userHomeCommand(user, herdrVerified+`; integration_status=$("$HOME/.local/bin/mise" exec -- herdr integration status); printf '%s\n' "$integration_status" | grep -Fq 'pi: current (v6)'; printf 'pi: current (v6)\n'`)},
	}
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
