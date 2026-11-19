package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/poll"
	"github.com/sagmans/serverpro/internal/rebootgate"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/state"
)

// UpgradeMode selects which remote OS upgrade workflow runs over Tailscale SSH.
type UpgradeMode string

const (
	// UpgradeModeAll refreshes apt metadata and installs all eligible package
	// upgrades, including new kernel ABI packages without removing packages.
	UpgradeModeAll UpgradeMode = "all"
	// UpgradeModeSecurity applies only Ubuntu security-origin upgrades through
	// unattended-upgrade so routine feature updates stay untouched.
	UpgradeModeSecurity UpgradeMode = "security"
	// UpgradeModeCheck refreshes apt metadata and simulates upgrades without
	// mutating installed packages or rebooting the host.
	UpgradeModeCheck UpgradeMode = "check"

	// upgradeCommandTimeout bounds a remote apt refresh and upgrade run so a
	// stalled mirror or dpkg prompt cannot hang the operator indefinitely.
	upgradeCommandTimeout = 30 * time.Minute

	// Structured line markers keep Go parsing independent of apt or locale prose.
	markerPendingPkg     = "SERVERPRO_PENDING_PKG="
	markerUpgradedPkg    = "SERVERPRO_UPGRADED_PKG="
	markerRebootRequired = "SERVERPRO_REBOOT_REQUIRED="
	markerRebootPkg      = "SERVERPRO_REBOOT_PKG="
	markerTrueValue      = "1"

	// maintenanceRootPath restricts root script execution to system directories
	// so user PATH entries never hijack apt, dpkg, or systemd binaries.
	maintenanceRootPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	// upgradeManagedHoldPackages lists apt packages owned by dedicated serverpro
	// updaters: upgrading tailscale over Tailscale SSH restarts tailscaled and
	// drops the active session, and upgrading cloudflared outside doctor --fix
	// invalidates /var/lib/serverpro/cloudflared.receipt.
	upgradeManagedHoldPackages = "tailscale cloudflared"
	upgradeManagedPinPriority  = "1001"
	aptSystemPreferencesPath   = "/etc/apt/preferences"

	// needrestart hooks restart daemons synchronously inside apt's Post-Invoke.
	// Suspending the hook during apt and running needrestart with tailscaled
	// excluded lets other daemons pick up patched libraries without killing the
	// active Tailscale SSH session.
	needrestartBinaryPath    = "/usr/sbin/needrestart"
	needrestartConfigPath    = "/etc/needrestart/needrestart.conf"
	needrestartUnpackedFlag  = "/run/needrestart/unpacked"
	needrestartTailscaleRule = `$nrconf{override_rc}->{q(^tailscaled\.service$)} = 0;`
)

// UpgradeOptions controls an on-demand OS package upgrade on a managed server.
type UpgradeOptions struct {
	Mode        UpgradeMode
	Reboot      bool
	ForceReboot bool
	NoWait      bool
	Wait        poll.WaitFunc
}

// UpgradeResult captures the observable outcome of an OS upgrade or check run.
type UpgradeResult struct {
	Mode                   UpgradeMode   `json:"mode"`
	PendingPackages        []string      `json:"pending_packages,omitempty"`
	UpgradedPackages       []string      `json:"upgraded_packages,omitempty"`
	RebootRequired         bool          `json:"reboot_required"`
	RebootRequiredPackages []string      `json:"reboot_required_packages,omitempty"`
	Reboot                 *RebootResult `json:"reboot,omitempty"`
}

// Valid reports whether m is one of the supported OS upgrade modes.
func (m UpgradeMode) Valid() bool {
	switch m {
	case UpgradeModeAll, UpgradeModeSecurity, UpgradeModeCheck:
		return true
	default:
		return false
	}
}

// UpgradeServer runs a scoped OS upgrade or read-only pending-upgrade check on
// a managed Ubuntu host and optionally performs a gated reboot when required.
func UpgradeServer(ctx context.Context, r remote.Runner, cfg config.Config, st state.State, opts UpgradeOptions) (UpgradeResult, error) {
	if err := validateUpgradeRequest(r, st, opts); err != nil {
		return UpgradeResult{}, err
	}
	script := upgradeScript(opts.Mode)
	out, err := remote.WithTimeout(r, upgradeCommandTimeout).Run(ctx, cfg.Admin.Username, st.Tailscale.Name, script)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("upgrade %s: %w", opts.Mode, err)
	}
	result := parseUpgradeOutput(opts.Mode, out)
	if !opts.Reboot || !result.RebootRequired {
		return result, nil
	}
	rebootRes, err := RebootServer(ctx, r, cfg, st, RebootOptions{
		IfRequired: true,
		Force:      opts.ForceReboot,
		NoWait:     opts.NoWait,
		Wait:       opts.Wait,
	})
	result.Reboot = &rebootRes
	if err != nil {
		return result, err
	}
	return result, nil
}

func validateUpgradeRequest(r remote.Runner, st state.State, opts UpgradeOptions) error {
	if r == nil || st.Tailscale.Name == "" {
		return fmt.Errorf("remote host unavailable for upgrade")
	}
	if !opts.Mode.Valid() {
		return fmt.Errorf("unsupported upgrade mode %q (want all, security, or check)", opts.Mode)
	}
	if opts.Mode == UpgradeModeCheck && (opts.Reboot || opts.ForceReboot || opts.NoWait) {
		return fmt.Errorf("check mode cannot be combined with reboot options")
	}
	if !opts.Reboot && (opts.ForceReboot || opts.NoWait) {
		return fmt.Errorf("reboot modifier flags require reboot to be enabled")
	}
	return nil
}

// upgradeScript renders the root POSIX shell script for one upgrade mode.
// Package state is diffed via dpkg-query before and after upgrades so reported
// changes reflect what actually landed rather than apt's interactive prose.
func upgradeScript(mode UpgradeMode) string {
	var b strings.Builder
	b.WriteString(maintenanceScriptHeader())
	b.WriteString(hostplatform.APTUpdateShellFunction())
	b.WriteString(supportedUbuntuGuard())
	b.WriteString("apt_update >/dev/null\n")
	writeUpgradeTempDirPrologue(&b)
	switch mode {
	case UpgradeModeCheck:
		writeCheckUpgradeBody(&b)
	case UpgradeModeSecurity:
		writeSecurityUpgradeBody(&b)
	default:
		writeFullUpgradeBody(&b)
	}
	b.WriteString(emitRebootRequiredMarkers())
	return b.String()
}

func maintenanceScriptHeader() string {
	return "set -eu\n" +
		"export LC_ALL=C DEBIAN_FRONTEND=noninteractive\n" +
		"PATH=" + maintenanceRootPath + "\n" +
		"export PATH\n"
}

// supportedUbuntuGuard fails closed before any package mutation so an
// unsupported OS or codename never receives Ubuntu-specific apt flags.
func supportedUbuntuGuard() string {
	return "test -r /etc/os-release\n" +
		". /etc/os-release\n" +
		"actual_codename=${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}\n" +
		"test \"${ID:-}\" = " + shell.Quote(hostplatform.ManagedHostOS) + " || { printf 'unsupported host OS: %s\\n' \"${ID:-unknown}\" >&2; exit 1; }\n" +
		"test \"${VERSION_ID:-}\" = " + shell.Quote(hostplatform.ManagedHostVersion) + " || { printf 'unsupported host version: %s\\n' \"${VERSION_ID:-unknown}\" >&2; exit 1; }\n" +
		"test \"${actual_codename}\" = " + shell.Quote(hostplatform.ManagedHostCodename) + " || { printf 'unsupported host codename: %s\\n' \"${actual_codename:-unknown}\" >&2; exit 1; }\n"
}

func writeUpgradeTempDirPrologue(b *strings.Builder) {
	b.WriteString("tmp_dir=$(mktemp -d)\n")
	b.WriteString("cleanup_upgrade_tmp() { rm -rf \"$tmp_dir\"; }\n")
	b.WriteString("trap cleanup_upgrade_tmp EXIT\n")
}

// writeManagedUpgradePreferences pins installed serverpro-managed transport and
// ingress packages to their current versions for this apt invocation only,
// leaving /etc/apt and dpkg hold state untouched.
func writeManagedUpgradePreferences(b *strings.Builder) {
	prefsPath := shell.Quote(aptSystemPreferencesPath)
	b.WriteString("if [ -r " + prefsPath + " ]; then\n" +
		"  cat " + prefsPath + " >\"$tmp_dir/preferences\"\n" +
		"  printf '\\n' >>\"$tmp_dir/preferences\"\n" +
		"else\n" +
		"  : >\"$tmp_dir/preferences\"\n" +
		"fi\n" +
		"for pkg in " + upgradeManagedHoldPackages + "; do\n" +
		"  rec=$(dpkg-query -W -f='${db:Status-Status}|${Version}' \"$pkg\" 2>/dev/null || true)\n" +
		"  case \"$rec\" in\n" +
		"    installed'|'*)\n" +
		"      ver=${rec#installed|}\n" +
		"      printf 'Package: %s\\nPin: version %s\\nPin-Priority: " + upgradeManagedPinPriority + "\\n\\n' \"$pkg\" \"$ver\" >>\"$tmp_dir/preferences\"\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\n")
}

func writeCheckUpgradeBody(b *strings.Builder) {
	writeManagedUpgradePreferences(b)
	b.WriteString("sim_out=$(apt-get -s -o Debug::NoLocking=1 -o Dir::Etc::Preferences=\"$tmp_dir/preferences\" --with-new-pkgs upgrade 2>&1)\n")
	b.WriteString("printf '%s\\n' \"$sim_out\" | awk '/^Inst / { print \"" + markerPendingPkg + "\" $2 }'\n")
}

func writeFullUpgradeBody(b *strings.Builder) {
	writeManagedUpgradePreferences(b)
	writePackageSnapshotBefore(b)
	// --with-new-pkgs installs new kernel ABI security packages without ever
	// removing installed packages, unlike dist-upgrade.
	b.WriteString("NEEDRESTART_SUSPEND=1 apt-get -y --with-new-pkgs -o Dir::Etc::Preferences=\"$tmp_dir/preferences\" -o " + shell.Quote(hostplatform.APTLockTimeoutOption()) +
		" -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade >/dev/null\n")
	writeSafeNeedrestartStep(b)
	writePackageSnapshotAfter(b)
}

func writeSecurityUpgradeBody(b *strings.Builder) {
	writePackageSnapshotBefore(b)
	// Waiting on dpkg lock first avoids failing unattended-upgrade immediately
	// when apt-daily holds the frontend lock.
	b.WriteString("apt-get -o " + shell.Quote(hostplatform.APTLockTimeoutOption()) + " check >/dev/null\n")
	b.WriteString("NEEDRESTART_SUSPEND=1 unattended-upgrade -v >/dev/null\n")
	writeSafeNeedrestartStep(b)
	writePackageSnapshotAfter(b)
}

func writeSafeNeedrestartStep(b *strings.Builder) {
	binPath := shell.Quote(needrestartBinaryPath)
	cfgPath := shell.Quote(needrestartConfigPath)
	flagPath := shell.Quote(needrestartUnpackedFlag)
	b.WriteString("if [ -x " + binPath + " ] && [ -r " + cfgPath + " ]; then\n" +
		"  rm -f " + flagPath + "\n" +
		"  cat " + cfgPath + " >\"$tmp_dir/needrestart.conf\"\n" +
		"  printf '\\n%s\\n' " + shell.Quote(needrestartTailscaleRule) + " >>\"$tmp_dir/needrestart.conf\"\n" +
		"  " + binPath + " -c \"$tmp_dir/needrestart.conf\" -r a >/dev/null\n" +
		"fi\n")
}

func writePackageSnapshotBefore(b *strings.Builder) {
	b.WriteString("dpkg-query -W -f='${Package}=${Version}\\n' | sort >\"$tmp_dir/before\"\n")
}

func writePackageSnapshotAfter(b *strings.Builder) {
	b.WriteString("dpkg-query -W -f='${Package}=${Version}\\n' | sort >\"$tmp_dir/after\"\n")
	b.WriteString("comm -13 \"$tmp_dir/before\" \"$tmp_dir/after\" | sed 's/^/" + markerUpgradedPkg + "/'\n")
}

func emitRebootRequiredMarkers() string {
	flagPath := shell.Quote(rebootgate.RebootRequiredPath)
	pkgsPath := shell.Quote(rebootgate.RebootRequiredPath + ".pkgs")
	return "if [ -e " + flagPath + " ]; then\n" +
		"  printf '%s\\n' '" + markerRebootRequired + markerTrueValue + "'\n" +
		"  if [ -r " + pkgsPath + " ]; then\n" +
		"    sort -u " + pkgsPath + " | awk 'NF { print \"" + markerRebootPkg + "\" $0 }'\n" +
		"  fi\n" +
		"else\n" +
		"  printf '%s\\n' '" + markerRebootRequired + "0'\n" +
		"fi\n"
}

func parseUpgradeOutput(mode UpgradeMode, out string) UpgradeResult {
	res := UpgradeResult{Mode: mode}
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case strings.HasPrefix(line, markerPendingPkg):
			res.PendingPackages = appendNonEmpty(res.PendingPackages, strings.TrimPrefix(line, markerPendingPkg))
		case strings.HasPrefix(line, markerUpgradedPkg):
			res.UpgradedPackages = appendNonEmpty(res.UpgradedPackages, strings.TrimPrefix(line, markerUpgradedPkg))
		case strings.HasPrefix(line, markerRebootRequired):
			res.RebootRequired = strings.TrimPrefix(line, markerRebootRequired) == markerTrueValue
		case strings.HasPrefix(line, markerRebootPkg):
			res.RebootRequiredPackages = appendNonEmpty(res.RebootRequiredPackages, strings.TrimPrefix(line, markerRebootPkg))
		}
	}
	return res
}

func appendNonEmpty(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	return append(items, value)
}
