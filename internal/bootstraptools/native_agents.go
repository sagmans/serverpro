package bootstraptools

import (
	"encoding/base64"
	"strconv"
)

// Agent CLIs (Pi, Herdr) ship their own updaters, and those updaters manage
// only the copy that the vendor installer created. A mise- or npm-managed copy
// breaks `pi update` and `herdr update`, so serverpro runs each vendor's
// native installer once and leaves later updates to the CLI and its operator.
// mise keeps the runtimes and tools that have no self-updater.
const (
	// PiInstallerURL is Pi's official installer for macOS and Linux. It
	// installs the vendor's managed layout that `pi update` maintains.
	PiInstallerURL = "https://pi.dev/install.sh"
	// HerdrInstallerURL is Herdr's official installer. It verifies the
	// release binary against the SHA-256 in Herdr's release manifest, and
	// `herdr update` maintains the copy it installs.
	HerdrInstallerURL = "https://herdr.dev/install.sh"

	// PiCheckName and HerdrCheckName carry no version: the CLIs update
	// themselves, so a pinned version in the name would read as drift.
	PiCheckName               = "pi"
	HerdrCheckName            = "herdr"
	HerdrPiIntegrationCheck   = "herdr pi integration"
	herdrPiIntegrationCurrent = "pi: current"

	// The vendor installers place their entrypoints here. The checks require
	// these exact paths so a stale mise or npm copy cannot shadow them.
	piEntrypoint    = `$HOME/.local/bin/pi`
	herdrEntrypoint = `$HOME/.local/bin/herdr`
	// piManagedMarker is the file that Pi's installer writes to mark a
	// managed install. `pi update` refuses to self-update without it.
	piManagedMarker     = `$HOME/.pi/agent/install/managed-install.json`
	piManagedMarkerKind = "pi-managed-install"

	// The probes travel as base64 manifest values so bootstrap readiness,
	// bootstrap verification, and doctor run one Go-owned copy of each check.
	piProbeEnv    = "SERVERPRO_BOOTSTRAP_PI_PROBE_BASE64"
	herdrProbeEnv = "SERVERPRO_BOOTSTRAP_HERDR_PROBE_BASE64"
	// herdrPiProbeEnv carries the integration probe for the same reason.
	herdrPiProbeEnv   = "SERVERPRO_BOOTSTRAP_HERDR_PI_PROBE_BASE64"
	piInstallerEnv    = "SERVERPRO_BOOTSTRAP_PI_INSTALLER_URL"
	herdrInstallerEnv = "SERVERPRO_BOOTSTRAP_HERDR_INSTALLER_URL"

	// semverPattern is the release format both vendors print. A self-updated
	// CLI may report any release, so the checks prove only that it runs.
	semverPattern = `^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$`
)

// nativeAgentEnvPairs is the manifest slice for the vendor-installed agent
// CLIs: the installer origins and the shared readiness probes.
func nativeAgentEnvPairs() [][2]string {
	return [][2]string{
		{piInstallerEnv, PiInstallerURL},
		{herdrInstallerEnv, HerdrInstallerURL},
		{piProbeEnv, base64.StdEncoding.EncodeToString([]byte(piCheckCommand()))},
		{herdrProbeEnv, base64.StdEncoding.EncodeToString([]byte(herdrCheckCommand()))},
		{herdrPiProbeEnv, base64.StdEncoding.EncodeToString([]byte(herdrPiIntegrationCommand()))},
	}
}

// piCheckCommand proves that the pi a mise-activated shell resolves is the
// vendor-managed install, so `pi update` can maintain it, and that it runs.
// Resolution goes through `mise exec` because an activated shell puts the
// Node bin directory first, where a legacy npm-installed pi would win.
func piCheckCommand() string {
	return `expected_pi="` + piEntrypoint + `"; actual_pi=$("$HOME/.local/bin/mise" exec -- sh -c 'command -v pi') || actual_pi=; ` +
		`test "$actual_pi" = "$expected_pi" || { printf 'expected pi at %s, got %s; rerun managed bootstrap\n' "$expected_pi" "${actual_pi:-<none>}" >&2; exit 1; }; ` +
		`marker="` + piManagedMarker + `"; grep -Fq ` + strconv.Quote(`"kind": "`+piManagedMarkerKind+`"`) + ` "$marker" 2>/dev/null || { printf 'pi is not a vendor-managed install (marker %s missing); pi update cannot maintain it; rerun managed bootstrap\n' "$marker" >&2; exit 1; }; ` +
		`pi_version=$("$HOME/.local/bin/mise" exec -- pi --version 2>&1) || { status=$?; printf 'pi --version failed (%s): %s\n' "$status" "$pi_version" >&2; exit "$status"; }; ` +
		`printf '%s\n' "$pi_version" | grep -Eq '` + semverPattern + `' || { printf 'pi --version printed an unexpected value: %s\n' "$pi_version" >&2; exit 1; }; ` +
		`printf 'pi %s (vendor-managed; update with pi update)\n' "$pi_version"`
}

// herdrCheckCommand proves that the herdr a mise-activated shell resolves is
// the installer-managed binary that `herdr update` maintains, and that it runs.
func herdrCheckCommand() string {
	return `expected_herdr="` + herdrEntrypoint + `"; actual_herdr=$("$HOME/.local/bin/mise" exec -- sh -c 'command -v herdr') || actual_herdr=; ` +
		`test "$actual_herdr" = "$expected_herdr" || { printf 'expected herdr at %s, got %s; rerun managed bootstrap\n' "$expected_herdr" "${actual_herdr:-<none>}" >&2; exit 1; }; ` +
		`test -f "$expected_herdr" && test -x "$expected_herdr" || { printf 'herdr at %s is not an executable file\n' "$expected_herdr" >&2; exit 1; }; ` +
		`herdr_version=$("$expected_herdr" --version 2>&1) || { status=$?; printf 'herdr --version failed (%s): %s\n' "$status" "$herdr_version" >&2; exit "$status"; }; ` +
		`printf '%s\n' "${herdr_version#herdr }" | grep -Eq '` + semverPattern + `' || { printf 'herdr --version printed an unexpected value: %s\n' "$herdr_version" >&2; exit 1; }; ` +
		`printf '%s (vendor-managed; update with herdr update)\n' "$herdr_version"`
}

// herdrPiIntegrationCommand proves that Herdr's Pi hook is installed and
// current, so Herdr can report Pi agent state. It gates on the identity check
// first, so a shadowing copy fails as itself, not as a hook problem, and
// bootstrap and doctor share one composed probe. The raw status names local
// paths, so failures print a fixed message instead of it.
func herdrPiIntegrationCommand() string {
	return `{ ` + herdrCheckCommand() + `; } >/dev/null; ` +
		`integration_status=$("` + herdrEntrypoint + `" integration status); ` +
		`printf '%s\n' "$integration_status" | grep -Fq '` + herdrPiIntegrationCurrent + `' || { printf 'herdr pi integration is missing or stale; rerun managed bootstrap\n' >&2; exit 1; }; ` +
		`printf '` + herdrPiIntegrationCurrent + `\n'`
}
