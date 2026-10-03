package bootstraptools

import "github.com/sagmans/serverpro/internal/shell"

// Pi's published shrinkwrap keeps a vulnerable dependency even when its CLI version matches.
// Resolving metadata avoids executing unverified dependency code during readiness checks.
const piBraceExpansionProbe = `const tool = "brace-expansion";
const { createRequire } = require("node:module");
const { readFileSync } = require("node:fs");
const [root, expectedVersion, expectedIntegrity] = process.argv.slice(1);
const fromPi = createRequire(root + "/package.json");
const fromMinimatch = createRequire(fromPi.resolve("minimatch"));
const installed = JSON.parse(readFileSync(fromMinimatch.resolve(tool + "/package.json"), "utf8"));
const lock = JSON.parse(readFileSync(root + "/npm-shrinkwrap.json", "utf8"));
const locked = lock.packages?.["node_modules/" + tool];
if (installed.version !== expectedVersion || locked?.version !== expectedVersion || locked?.integrity !== expectedIntegrity) {
  console.error("expected Pi " + tool + " " + expectedVersion + " with reviewed integrity, got " + installed.version);
  process.exit(1);
}
console.log(tool + " " + installed.version);`

func piBraceExpansionCheckCommand() string {
	return `"$HOME/.local/bin/mise" exec -- node -e ` + shell.Quote(piBraceExpansionProbe) +
		` "$HOME/.local/share/mise/installs/node/` + NodeVersion + `/lib/node_modules/` + PiToolName + `" ` +
		shell.Quote(PiBraceExpansionVersion) + " " + shell.Quote(PiBraceExpansionIntegrity)
}
