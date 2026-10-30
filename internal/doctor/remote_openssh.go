package doctor

import (
	"strconv"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	openSSHDisabledCheckName   = "openssh disabled"
	openSSHDisabledRemediation = "OpenSSH must stay off while Tailscale SSH is the admin path: run serverpro server doctor --fix, which checks that Tailscale SSH serves the host and that nobody uses OpenSSH before it masks the ssh units, or set access.openssh: enabled to keep OpenSSH (USAGE.md, OpenSSH off)"
	openSSHEnabledSkip         = "access.openssh is enabled"
)

// remoteOpenSSHSpecification holds a host to the config's OpenSSH mode. The
// repair is the provisioning script itself, preflight included, so doctor
// --fix turns OpenSSH off only under the same gates as a create. A config
// that opts back in skips the check instead of passing it: doctor did not
// look, and an enabled sshd is then the operator's decision.
func remoteOpenSSHSpecification(cfg config.Config, consoleAgents []string) remoteCheckSpecification {
	if cfg.Access.OpenSSH == config.OpenSSHEnabled {
		return remoteStaticSpecification(skip("remote", openSSHDisabledCheckName, openSSHEnabledSkip))
	}
	return remoteFixableDriftSpecification(openSSHDisabledCheckName, openSSHDisabledCommand(), openSSHDisabledRemediation, lifecycle.OpenSSHDisableScript(cfg.Admin.Username, consoleAgents))
}

// openSSHDisabledCommand fails while either ssh unit is enabled or active, or
// anything listens where sshd would. A unit that is only disabled warns: the
// socket or a dependency can still start it, which masking prevents. A
// missing condition file warns, because it is the guard left when someone
// unmasks a unit by hand. An empty is-enabled answer means systemctl itself
// failed, so the probe exits non-zero instead of reading as a clean host.
func openSSHDisabledCommand() string {
	return "export LC_ALL=C\n" +
		"for unit in " + hostplatform.OpenSSHSocketUnit + " " + hostplatform.OpenSSHServiceUnit + "; do\n" +
		"  unit_state=$(systemctl is-enabled \"$unit\" 2>/dev/null) || true\n" +
		"  case \"$unit_state\" in\n" +
		"    masked|masked-runtime|not-found) ;;\n" +
		"    disabled) printf '" + driftWarnPrefix + "%s is disabled but not masked\\n' \"$unit\" ;;\n" +
		"    '') exit 1 ;;\n" +
		"    *) printf '" + driftFailPrefix + "%s is %s\\n' \"$unit\" \"$unit_state\" ;;\n" +
		"  esac\n" +
		"  if systemctl is-active --quiet \"$unit\"; then printf '" + driftFailPrefix + "%s is active\\n' \"$unit\"; fi\n" +
		"done\n" +
		"names=$(" + lifecycle.OpenSSHListenersCommand() + ") || exit 1\n" +
		"for name in $names; do printf '" + driftFailPrefix + "%s listens on port " + strconv.Itoa(lifecycle.OpenSSHPort) + " or as sshd\\n' \"$name\"; done\n" +
		"[ -e " + shell.Quote(lifecycle.OpenSSHConditionPath) + " ] || printf '%s\\n' " + shell.Quote(driftWarnPrefix+lifecycle.OpenSSHConditionPath+" is absent") + "\n" +
		"printf '%s\\n' 'ssh units off; no sshd listener'"
}
