package bootstraptools

import (
	"fmt"
	"strconv"
	"strings"
)

const managedMiseManifestEnv = "SERVERPRO_BOOTSTRAP_MISE_TOOLS"

type managedMiseProbe string

const (
	managedMiseProbeNode    managedMiseProbe = "node"
	managedMiseProbeUV      managedMiseProbe = "uv"
	managedMiseProbeRust    managedMiseProbe = "rust"
	managedMiseProbeTmux    managedMiseProbe = "tmux"
	managedMiseProbeGH      managedMiseProbe = "gh"
	managedMiseProbeRG      managedMiseProbe = "rg"
	managedMiseProbeFD      managedMiseProbe = "fd"
	managedMiseProbeAstGrep managedMiseProbe = "ast-grep"
	managedMiseProbeSem     managedMiseProbe = "sem"
	managedMiseProbeInspect managedMiseProbe = "inspect"
)

// managedMiseTool carries per-architecture release digests so bootstrap never
// trusts whichever mutable asset a remote registry happens to serve.
type managedMiseTool struct {
	key              string
	version          string
	versionEnv       string
	aliasKey         string
	backend          string
	backendEnv       string
	versionKey       string
	profileKey       string
	profile          string
	profileEnv       string
	checksumKey      string
	checksumX64      string
	checksumX64Env   string
	checksumArm64    string
	checksumArm64Env string
	forceRepair      bool
	probe            managedMiseProbe
}

// managedMiseTools is the authority for ordinary mise-managed tools. Pi and
// Herdr stay separate because npm lifecycle policy and binary digest checks
// require purpose-built repair flows.
var managedMiseTools = []managedMiseTool{
	{key: "node", version: NodeVersion, versionEnv: "SERVERPRO_BOOTSTRAP_NODE_VERSION", versionKey: "tools.node", probe: managedMiseProbeNode},
	{key: "uv", version: UVVersion, versionEnv: "SERVERPRO_BOOTSTRAP_UV_VERSION", aliasKey: "tool_alias.uv", backend: UVMiseBackend, backendEnv: "SERVERPRO_BOOTSTRAP_UV_BACKEND", versionKey: "tools.uv", probe: managedMiseProbeUV},
	{key: "rust", version: RustVersion, versionEnv: "SERVERPRO_BOOTSTRAP_RUST_VERSION", aliasKey: "tool_alias.rust", backend: RustMiseBackend, backendEnv: "SERVERPRO_BOOTSTRAP_RUST_BACKEND", versionKey: "tools.rust.version", profileKey: "tools.rust.profile", profile: RustProfile, profileEnv: "SERVERPRO_BOOTSTRAP_RUST_PROFILE", forceRepair: true, probe: managedMiseProbeRust},
	{key: "tmux", version: TmuxVersion, versionEnv: "SERVERPRO_BOOTSTRAP_TMUX_VERSION", versionKey: "tools.tmux", probe: managedMiseProbeTmux},
	{key: "gh", version: GitHubCLIVersion, versionEnv: "SERVERPRO_BOOTSTRAP_GH_VERSION", versionKey: "tools.gh", probe: managedMiseProbeGH},
	{key: "rg", version: RipgrepVersion, versionEnv: "SERVERPRO_BOOTSTRAP_RG_VERSION", versionKey: "tools.rg", probe: managedMiseProbeRG},
	{key: "fd", version: FdVersion, versionEnv: "SERVERPRO_BOOTSTRAP_FD_VERSION", versionKey: "tools.fd", probe: managedMiseProbeFD},
	{key: "ast-grep", version: AstGrepVersion, versionEnv: "SERVERPRO_BOOTSTRAP_AST_GREP_VERSION", aliasKey: "tool_alias.ast-grep", backend: AstGrepMiseBackend, backendEnv: "SERVERPRO_BOOTSTRAP_AST_GREP_BACKEND", versionKey: "tools.ast-grep.version", checksumKey: "tools.ast-grep.checksum", checksumX64: AstGrepLinuxX64SHA256, checksumX64Env: "SERVERPRO_BOOTSTRAP_AST_GREP_SHA256_LINUX_X64", checksumArm64: AstGrepLinuxArm64SHA256, checksumArm64Env: "SERVERPRO_BOOTSTRAP_AST_GREP_SHA256_LINUX_ARM64", forceRepair: true, probe: managedMiseProbeAstGrep},
	{key: "sem", version: SemVersion, versionEnv: "SERVERPRO_BOOTSTRAP_SEM_VERSION", aliasKey: "tool_alias.sem", backend: SemMiseBackend, backendEnv: "SERVERPRO_BOOTSTRAP_SEM_BACKEND", versionKey: "tools.sem.version", checksumKey: "tools.sem.checksum", checksumX64: SemLinuxX64SHA256, checksumX64Env: "SERVERPRO_BOOTSTRAP_SEM_SHA256_LINUX_X64", checksumArm64: SemLinuxArm64SHA256, checksumArm64Env: "SERVERPRO_BOOTSTRAP_SEM_SHA256_LINUX_ARM64", forceRepair: true, probe: managedMiseProbeSem},
	{key: "inspect", version: InspectVersion, versionEnv: "SERVERPRO_BOOTSTRAP_INSPECT_VERSION", aliasKey: "tool_alias.inspect", backend: InspectMiseBackend, backendEnv: "SERVERPRO_BOOTSTRAP_INSPECT_BACKEND", versionKey: "tools.inspect.version", checksumKey: "tools.inspect.checksum", checksumX64: InspectLinuxX64SHA256, checksumX64Env: "SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_X64", checksumArm64: InspectLinuxArm64SHA256, checksumArm64Env: "SERVERPRO_BOOTSTRAP_INSPECT_SHA256_LINUX_ARM64", forceRepair: true, probe: managedMiseProbeInspect},
}

func (t managedMiseTool) checkName() string {
	return t.key + " " + t.version
}

func managedMiseEnvPairs() [][2]string {
	pairs := make([][2]string, 0, len(managedMiseTools)*5+1)
	for _, tool := range managedMiseTools {
		pairs = append(pairs, [2]string{tool.versionEnv, tool.version})
		if tool.backendEnv != "" {
			pairs = append(pairs, [2]string{tool.backendEnv, tool.backend})
		}
		if tool.profileEnv != "" {
			pairs = append(pairs, [2]string{tool.profileEnv, tool.profile})
		}
		if tool.checksumX64Env != "" {
			pairs = append(pairs,
				[2]string{tool.checksumX64Env, tool.checksumX64},
				[2]string{tool.checksumArm64Env, tool.checksumArm64},
			)
		}
	}
	return append(pairs, [2]string{managedMiseManifestEnv, managedMiseManifest()})
}

func managedMiseManifest() string {
	var rows strings.Builder
	for i, tool := range managedMiseTools {
		if i > 0 {
			rows.WriteByte('\n')
		}
		rows.WriteString(strings.Join([]string{
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
		}, "|"))
	}
	return rows.String()
}

func emptyManifestField(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func managedMiseProbeCommand(tool managedMiseTool) string {
	prefix := "expected_version=" + strconv.Quote(tool.version) + `; mise_bin="$HOME/.local/bin/mise"; `
	var probe string
	switch tool.probe {
	case managedMiseProbeNode:
		probe = `node_version=$("$mise_bin" exec -- node --version); test "$node_version" = "v$expected_version" || { printf 'expected node %s, got %s\n' "$expected_version" "$node_version" >&2; exit 1; }; npm_version=$("$mise_bin" exec -- npm --version); printf '%s\n%s\n' "$node_version" "$npm_version"`
	case managedMiseProbeUV:
		probe = `uv_version=$("$mise_bin" exec -- uv --version); case "$uv_version" in "uv $expected_version"|"uv $expected_version "*) ;; *) printf 'expected uv %s, got %s\n' "$expected_version" "$uv_version" >&2; exit 1 ;; esac; printf '%s\n' "$uv_version"`
	case managedMiseProbeRust:
		probe = `rustc_version=$("$mise_bin" exec -- rustc --version); case "$rustc_version" in "rustc $expected_version "*) ;; *) printf 'expected rustc %s, got %s\n' "$expected_version" "$rustc_version" >&2; exit 1 ;; esac; cargo_version=$("$mise_bin" exec -- cargo --version); rustfmt_version=$("$mise_bin" exec -- rustfmt --version); clippy_version=$("$mise_bin" exec -- cargo clippy --version); rust_docs=$("$mise_bin" exec -- rustup component list --installed | grep -m1 '^rust-docs-'); test -n "$rust_docs"; printf '%s\n%s\n%s\n%s\n%s\n' "$rustc_version" "$cargo_version" "$rustfmt_version" "$clippy_version" "$rust_docs"`
	case managedMiseProbeTmux:
		probe = `tool_version=$("$mise_bin" exec -- tmux -V); test "$tool_version" = "tmux $expected_version" || { printf 'expected tmux %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"`
	case managedMiseProbeGH:
		probe = `tool_version=$("$mise_bin" exec -- gh --version | head -n1); set -- $tool_version; test "${3:-}" = "$expected_version" || { printf 'expected gh %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"`
	case managedMiseProbeRG:
		probe = `tool_version=$("$mise_bin" exec -- rg --version | head -n1); case "$tool_version" in "ripgrep $expected_version"|"ripgrep $expected_version "*) ;; *) printf 'expected ripgrep %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1 ;; esac; printf '%s\n' "$tool_version"`
	case managedMiseProbeFD:
		probe = `tool_version=$("$mise_bin" exec -- fd --version); test "$tool_version" = "fd $expected_version" || { printf 'expected fd %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"`
	case managedMiseProbeAstGrep:
		probe = `tool_version=$("$mise_bin" exec -- ast-grep --version); case "$tool_version" in "ast-grep $expected_version"|"ast-grep $expected_version "*) ;; *) printf 'expected ast-grep %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1 ;; esac; printf '%s\n' "$tool_version"`
	case managedMiseProbeSem:
		probe = `tool_version=$("$mise_bin" exec -- sem --version); test "$tool_version" = "sem $expected_version" || { printf 'expected sem %s, got %s\n' "$expected_version" "$tool_version" >&2; exit 1; }; printf '%s\n' "$tool_version"`
	case managedMiseProbeInspect:
		// Upstream exposes no version flag, so prove artifact identity before
		// invoking even its help path.
		probe = managedMiseExpectedSHACommand(tool) + `; inspect_bin=$("$mise_bin" exec -- sh -c 'command -v inspect'); test -f "$inspect_bin" && test -x "$inspect_bin"; actual_sha=$(sha256sum "$inspect_bin" | awk '{print $1}'); test "$actual_sha" = "$expected_sha" || { printf 'inspect SHA-256 mismatch: expected %s, got %s\n' "$expected_sha" "$actual_sha" >&2; exit 1; }; help=$("$mise_bin" exec -- inspect --help); printf '%s\n' "$help" | grep -Fq 'Entity-level code review'; printf 'inspect %s\nsha256 %s\n' "$expected_version" "$actual_sha"`
	default:
		panic(fmt.Sprintf("unsupported managed mise probe %q", tool.probe))
	}
	return prefix + probe
}

func managedMiseExpectedSHACommand(tool managedMiseTool) string {
	if tool.checksumX64 == "" || tool.checksumArm64 == "" {
		panic(fmt.Sprintf("managed mise probe %q requires release checksums", tool.probe))
	}
	return `case "$(uname -m)" in x86_64) expected_sha=` + tool.checksumX64 + ` ;; aarch64|arm64) expected_sha=` + tool.checksumArm64 + ` ;; *) printf 'unsupported architecture for ` + tool.key + ` release: %s\n' "$(uname -m)" >&2; exit 1 ;; esac`
}
