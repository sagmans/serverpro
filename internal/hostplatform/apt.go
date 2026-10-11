package hostplatform

import (
	"strconv"
	"time"

	"github.com/sagmans/serverpro/internal/shell"
)

const (
	// APTLockTimeoutSeconds is the one wait budget every managed-host apt call
	// spends on a concurrent apt run (apt-daily, unattended-upgrades, an
	// operator) before failing, so provisioning and repair tolerate routine
	// background maintenance without hanging indefinitely.
	APTLockTimeoutSeconds = 300
	// APTLockTimeout is the same budget for controller-side deadlines that
	// must outlast the remote apt waits they wrap.
	APTLockTimeout = APTLockTimeoutSeconds * time.Second
	// APTListsLockRetrySeconds spaces apt-get update retries while the lists
	// lock is held; apt offers no wait for that lock, so the scripts poll.
	APTListsLockRetrySeconds = 5
	// APTListsLockError is apt's C-locale message for a held package-lists
	// lock. Scripts retry only on it so repository or network failures still
	// fail at once instead of burning the wait budget.
	APTListsLockError = "Could not get lock /var/lib/apt/lists/lock"
	// aptLockTimeoutOptionName is apt's dpkg-lock wait setting. It covers only
	// the dpkg frontend and admin locks, never the package-lists lock.
	aptLockTimeoutOptionName = "DPkg::Lock::Timeout"
)

// APTLockTimeoutOption renders the dpkg-lock wait as an apt-get `-o` value so
// install, upgrade, and remove calls share the reviewed budget.
func APTLockTimeoutOption() string {
	return aptLockTimeoutOptionName + "=" + strconv.Itoa(APTLockTimeoutSeconds)
}

// APTUpdateShellFunction renders a POSIX sh `apt_update` function for the
// Go-rendered host scripts. apt-get update takes only the package-lists lock,
// which DPkg::Lock::Timeout does not cover, so a running apt-daily would
// otherwise fail the refresh at once. The function retries only that lock
// error within APTLockTimeoutSeconds; apt's stdout streams, its stderr is
// replayed after each attempt, and any other failure keeps apt's status.
// Values are embedded so the function works in scripts without a shared
// header, and LC_ALL=C is forced because the match reads apt's message.
func APTUpdateShellFunction() string {
	return `# apt_update waits for a concurrent apt run that holds the package-lists lock.
apt_update() {
  apt_update_budget=` + strconv.Itoa(APTLockTimeoutSeconds) + `
  apt_update_retry=` + strconv.Itoa(APTListsLockRetrySeconds) + `
  apt_update_lock_error=` + shell.Quote(APTListsLockError) + `
  apt_update_waited=0
  while :; do
    apt_update_status=0
    # stderr is captured for the lock match while stdout streams through fd 3.
    { apt_update_errors=$(LC_ALL=C DEBIAN_FRONTEND=noninteractive apt-get update 2>&1 1>&3 3>&-); } 3>&1 || apt_update_status=$?
    if [ "${apt_update_status}" -eq 0 ]; then
      if [ -n "${apt_update_errors}" ]; then
        printf '%s\n' "${apt_update_errors}" >&2
      fi
      return 0
    fi
    case "${apt_update_errors}" in
      *"${apt_update_lock_error}"*) ;;
      *)
        printf '%s\n' "${apt_update_errors}" >&2
        return "${apt_update_status}"
        ;;
    esac
    if [ "${apt_update_waited}" -ge "${apt_update_budget}" ]; then
      printf '%s\n' "${apt_update_errors}" >&2
      printf 'apt package lists stayed locked for %s seconds; another apt run still holds them\n' "${apt_update_waited}" >&2
      return "${apt_update_status}"
    fi
    if [ "${apt_update_waited}" -eq 0 ]; then
      printf '%s\n' "${apt_update_errors}" >&2
      printf 'waiting up to %s seconds for the apt package lists lock\n' "${apt_update_budget}" >&2
    fi
    sleep "${apt_update_retry}"
    apt_update_waited=$((apt_update_waited + apt_update_retry))
  done
}
`
}
