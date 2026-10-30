package tunnel

import (
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/shell"
)

const (
	// PinCheckName is the doctor result for the apt origin pin. The connector
	// repair rewrites the pin only when the version or receipt is wrong, so a
	// deleted or edited pin stays invisible to the CheckName result while apt
	// may already prefer another repository's cloudflared.
	PinCheckName = "cloudflared apt pin"
	// IngressCheckName is the doctor result that holds a live connector
	// against config ingress `none`. The cloudflared check already audits such
	// a connector's floor and receipt; this result names the drift itself: a
	// tunnel that config does not own is public ingress nobody reviews.
	IngressCheckName = "cloudflared ingress"
	// hostStateLinePrefix marks HostStateCommand's answer, so ParseHostState
	// cannot read a state out of other output.
	hostStateLinePrefix = "cloudflared_host_state="
)

// HostState is the connector state cloudflared_host_state prints. The
// unmanaged check, its repair, and the ingress drift check all dispatch on it.
type HostState string

// The host states share the helper's spelling, so the doctor result and the
// unmanaged repair cannot disagree on a state's name.
const (
	HostStateAbsent  HostState = hostStateAbsent
	HostStateActive  HostState = hostStateActive
	HostStateDormant HostState = hostStateDormant
)

// HostStateCommand prints the connector state through the helper that the
// unmanaged check and repair use, so all three agree on what a live connector
// is. A host the helper cannot classify prints its reason and exits 1.
func HostStateCommand() string {
	return hostStateCommand(cloudflaredBinaryPath, cloudflaredReceiptPath)
}

// hostStateCommand takes the on-disk paths so tests can run the rendered read
// against a scratch host, as unmanagedCheckCommand does.
func hostStateCommand(binaryPath, receiptPath string) string {
	return checkVariables(binaryPath, receiptPath) + hostStateHelper + `if ! host_state=$(cloudflared_host_state "${package_name}" "${service_name}" "${binary_path}"); then
  printf '%s\n' "${host_state}"
  exit 1
fi
printf '` + hostStateLinePrefix + `%s\n' "${host_state}"`
}

// ParseHostState reads HostStateCommand output. ok is false for anything but
// one of the three states, so a garbled answer fails closed.
func ParseHostState(out string) (HostState, bool) {
	for _, line := range strings.Split(out, "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), hostStateLinePrefix)
		if !found {
			continue
		}
		switch state := HostState(value); state {
		case HostStateAbsent, HostStateActive, HostStateDormant:
			return state, true
		}
		return "", false
	}
	return "", false
}

// HostStateAnswer is HostStateCommand's output for state. Remote fakes use it
// because the doctor result parses the answer instead of trusting exit 0.
func HostStateAnswer(state HostState) string {
	return hostStateLinePrefix + string(state) + "\n"
}

// PinCheckCommand fails unless the apt preferences file holds exactly the pin
// install and repair write. A byte comparison is enough because serverpro is
// the only writer, and it catches a lowered priority or another origin that
// apt-cache policy would only show as a changed candidate.
func PinCheckCommand() string {
	return fmt.Sprintf(`export LC_ALL=C
pin_path=%[1]s
want=%[2]s
if [ ! -r "${pin_path}" ]; then
  printf 'cloudflared apt pin is missing: %%s\n' "${pin_path}"
  exit 1
fi
if ! printf '%%s' "${want}" | cmp -s - "${pin_path}"; then
  printf 'cloudflared apt pin %%s differs from serverpro'"'"'s origin %%s pin at priority %%s\n' "${pin_path}" %[3]s %[4]s
  exit 1
fi
printf 'cloudflared apt pin: origin %%s priority %%s\n' %[3]s %[4]s`,
		shell.Quote(cloudflaredAPTPreferencesPath),
		shell.Quote(cloudflaredAPTPreferences()),
		shell.Quote(cloudflareAPTOrigin()),
		shell.Quote(cloudflaredAPTPinPriority),
	)
}

// PinRepairScript rewrites only the pin, through the same staged rename that
// install and the connector repair use. It touches no package, service, or
// token, so it is safe to run on a host whose connector is otherwise healthy.
func PinRepairScript() string {
	return scriptPreamble() + `
main() {
  require_supported_host
  write_cloudflared_apt_pin
  printf 'cloudflared apt pin written: %s\n' "${CLOUDFLARED_APT_PREFERENCES_PATH}"
}
` + scriptFooter
}
