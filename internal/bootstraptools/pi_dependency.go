package bootstraptools

import (
	"strings"

	"github.com/sagmans/serverpro/internal/shell"
)

const (
	// The repair manifest and probe travel as environment values so bootstrap,
	// its local repair, and doctor all read one Go-owned list.
	piDependencyRepairsEnv = "SERVERPRO_BOOTSTRAP_PI_DEPENDENCY_REPAIRS"
	piDependencyProbeEnv   = "SERVERPRO_BOOTSTRAP_PI_DEPENDENCY_PROBE_BASE64"
	// piRootResolution marks a dependency Pi requires directly, so the probe
	// resolves it from Pi's own package root instead of another dependency.
	piRootResolution = "-"

	piBraceExpansionPackage = "brace-expansion"
	// Only minimatch loads brace-expansion, so its module context finds the
	// copy that Pi's code actually runs.
	piBraceExpansionResolveFrom = "minimatch"
	piUndiciPackage             = "undici"
)

// piDependencyRepair names one shrinkwrapped Pi runtime dependency that
// serverpro replaces locally because no compatible Pi release ships the fix.
type piDependencyRepair struct {
	pkg       string
	version   string
	integrity string
	// resolveFrom is the Pi dependency whose module context loads pkg, or
	// piRootResolution when Pi requires pkg itself.
	resolveFrom string
}

// piDependencyRepairs is the authority for local Pi dependency repairs. Pi's
// upstream shrinkwrap pins vulnerable brace-expansion and undici releases, and
// its CLI version alone cannot prove a host carries the reviewed replacements.
var piDependencyRepairs = []piDependencyRepair{
	{pkg: piBraceExpansionPackage, version: PiBraceExpansionVersion, integrity: PiBraceExpansionIntegrity, resolveFrom: piBraceExpansionResolveFrom},
	{pkg: piUndiciPackage, version: PiUndiciVersion, integrity: PiUndiciIntegrity, resolveFrom: piRootResolution},
}

// Pi's published shrinkwrap keeps vulnerable dependencies even when its CLI version matches.
// Resolving metadata avoids executing unverified dependency code during readiness checks.
// Each repair names the module context that loads it, so the probe checks the copy Pi actually runs.
const piDependencyProbe = `const { createRequire } = require("node:module");
const { readFileSync } = require("node:fs");
const [root, ...repairs] = process.argv.slice(1);
if (repairs.length === 0 || repairs.length % 4 !== 0) {
  console.error("invalid Pi dependency repair arguments");
  process.exit(1);
}
const fromPi = createRequire(root + "/package.json");
const lock = JSON.parse(readFileSync(root + "/npm-shrinkwrap.json", "utf8"));
for (let i = 0; i < repairs.length; i += 4) {
  const [tool, expectedVersion, expectedIntegrity, resolveFrom] = repairs.slice(i, i + 4);
  const from = resolveFrom === "` + piRootResolution + `" ? fromPi : createRequire(fromPi.resolve(resolveFrom));
  const installed = JSON.parse(readFileSync(from.resolve(tool + "/package.json"), "utf8"));
  const locked = lock.packages?.["node_modules/" + tool];
  if (installed.version !== expectedVersion || locked?.version !== expectedVersion || locked?.integrity !== expectedIntegrity) {
    console.error("expected Pi " + tool + " " + expectedVersion + " with reviewed integrity, got " + installed.version);
    process.exit(1);
  }
  console.log(tool + " " + installed.version);
}`

// piDependencyRepairManifest renders one `package|version|integrity|resolve-from`
// row per repair, the format the bootstrap shell validates and consumes.
func piDependencyRepairManifest() string {
	rows := make([]string, len(piDependencyRepairs))
	for i, repair := range piDependencyRepairs {
		rows[i] = strings.Join([]string{repair.pkg, repair.version, repair.integrity, repair.resolveFrom}, "|")
	}
	return strings.Join(rows, "\n")
}

// piDependencyCheckCommand is doctor's copy of the probe the bootstrap shell
// builds from the same manifest, so both reach one trust decision.
func piDependencyCheckCommand() string {
	var args strings.Builder
	for _, repair := range piDependencyRepairs {
		for _, field := range []string{repair.pkg, repair.version, repair.integrity, repair.resolveFrom} {
			args.WriteByte(' ')
			args.WriteString(shell.Quote(field))
		}
	}
	return `"$HOME/.local/bin/mise" exec -- node -e ` + shell.Quote(piDependencyProbe) +
		` "$HOME/.local/share/mise/installs/node/` + NodeVersion + `/lib/node_modules/` + PiToolName + `"` + args.String()
}
