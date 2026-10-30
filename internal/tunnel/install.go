// Package tunnel owns the Cloudflare Tunnel connector contract on managed
// hosts: the provisioning install, the doctor check, and the doctor repair all
// render from one set of shell helpers so trust pinning and the package floor
// cannot drift between them.
package tunnel

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	// CheckName is the doctor result name; doctor evidence summaries and
	// specifications key on it, so it lives next to the check it names.
	CheckName = "cloudflared"

	// scriptTransferMargin covers the connector script's own apt work: index
	// download, package download, and install once the locks are free.
	scriptTransferMargin = 5 * time.Minute
	// ScriptTimeout is the SSH deadline for the install and repair scripts.
	// Both may spend the full shared apt wait on the package-lists lock and
	// again on the dpkg lock before their own work, so provisioning and doctor
	// wrap them in this deadline instead of the short default, which would cut
	// off a wait the script would survive.
	ScriptTimeout = 2*hostplatform.APTLockTimeout + scriptTransferMargin

	cloudflareAPTKeyFingerprint = "CC94B39C77AE7342A68B89628A682D308D4E5E73"
	cloudflareAPTKeyURL         = "https://pkg.cloudflare.com/cloudflare-main.gpg"
	cloudflareAPTRepositoryURL  = "https://pkg.cloudflare.com/cloudflared"
	cloudflareAPTComponent      = "main"
	cloudflareKeyringPath       = "/usr/share/keyrings/cloudflare-main.gpg"
	cloudflaredAPTSourcePath    = "/etc/apt/sources.list.d/cloudflared.list"
	// cloudflareLegacyAPTSuite is the suite provisioning wrote before it
	// pinned the host codename. Hosts provisioned then still carry it, so
	// repair recognises exactly that one legacy line instead of refusing them.
	cloudflareLegacyAPTSuite = "any"
	// cloudflaredAPTPreferencesPath holds the origin pin rendered by
	// cloudflaredAPTPreferences; install and repair both (re)write it.
	cloudflaredAPTPreferencesPath = "/etc/apt/preferences.d/serverpro-cloudflared"
	// cloudflaredAPTPinPriority ranks the Cloudflare origin above the default
	// 500 of every other repository yet below 1000, so the pin never asks apt
	// to downgrade an installed newer connector.
	cloudflaredAPTPinPriority = "990"
	// stagingSuffix names the sibling file the pin and the install receipt are
	// staged in, so the rename that publishes each is atomic and neither apt
	// nor the doctor check ever reads a half-written file.
	stagingSuffix = ".serverpro-new"
	// cloudflaredReceiptPath records the version and binary digest of the
	// connector serverpro installed from the Cloudflare repository. The signed
	// index lists only the latest release, so once Cloudflare ships a newer
	// one apt can no longer vouch for the installed version; the receipt is
	// what still tells serverpro's build apart from a connector placed by
	// dpkg -i or another repository. It lives in cloud-init's serverpro state
	// directory.
	cloudflaredReceiptPath = "/var/lib/serverpro/cloudflared.receipt"
	// dpkgStatusPath is the pseudo-repository apt-cache policy lists for the
	// installed version. The origin gate skips it: it records what is already
	// on disk, not a source apt can fetch the candidate from.
	dpkgStatusPath         = "/var/lib/dpkg/status"
	cloudflaredServiceName = "cloudflared"
	cloudflaredServiceUser = "cloudflared"
	// CloudflaredConfigDir also holds any locally managed ingress file, which
	// the OpenSSH preflight reads before it turns sshd off.
	CloudflaredConfigDir  = "/etc/cloudflared"
	cloudflaredTokenPath  = CloudflaredConfigDir + "/token"
	cloudflaredUnitPath   = "/etc/systemd/system/cloudflared.service"
	cloudflaredBinaryPath = "/usr/bin/cloudflared"
)

// cloudflaredAPTSource is the exact source line provisioning writes. The
// repair compares against it so a hand-edited or signed-by-less source can
// never be treated as the trusted repository.
func cloudflaredAPTSource() string {
	return cloudflaredAPTSourceForSuite(hostplatform.ManagedHostCodename)
}

// cloudflaredLegacyAPTSource is the exact line older provisioning wrote. It
// names the same keyring, repository, and component as the current line, so
// migrating it to the codename suite keeps the trust anchor unchanged.
func cloudflaredLegacyAPTSource() string {
	return cloudflaredAPTSourceForSuite(cloudflareLegacyAPTSuite)
}

// cloudflaredAPTSourceForSuite keeps the current and legacy lines on one
// template, so they can differ only in the suite.
func cloudflaredAPTSourceForSuite(suite string) string {
	return "deb [signed-by=" + cloudflareKeyringPath + "] " + cloudflareAPTRepositoryURL + " " + suite + " " + cloudflareAPTComponent
}

// cloudflareAPTOrigin is the apt origin the pin names. apt takes a
// repository's origin from its URL host, so deriving it from
// cloudflareAPTRepositoryURL keeps the pin and the source line on one host.
func cloudflareAPTOrigin() string {
	repository, err := url.Parse(cloudflareAPTRepositoryURL)
	if err != nil || repository.Hostname() == "" {
		panic(fmt.Sprintf("invalid Cloudflare apt repository URL %q: %v", cloudflareAPTRepositoryURL, err))
	}
	return repository.Hostname()
}

// cloudflaredAPTPreferences makes apt prefer Cloudflare's cloudflared over
// the same package from any other configured repository. A pin only ranks
// versions; it excludes no source. Another repository's version still becomes
// the candidate when Cloudflare offers none or when the installed version is
// newer than Cloudflare's (a pin below 1000 never downgrades), so the
// install and repair choose their target from the versions only Cloudflare
// offers rather than from apt's candidate.
func cloudflaredAPTPreferences() string {
	return "Package: " + hostplatform.CloudflaredPackageBaseline().Name + "\nPin: origin \"" + cloudflareAPTOrigin() + "\"\nPin-Priority: " + cloudflaredAPTPinPriority + "\n"
}

// CheckCommand prints exactly one reason when the connector is unhealthy.
// Doctor evidence carries this output because the batch error alone only
// reports an exit status, which cannot tell a missing package from an
// outdated one, a connector serverpro did not install, or a stopped service.
// The provenance step compares the installed version and binary digest with
// the install receipt, so a dpkg -i or other-repository connector fails even
// above the package floor; it reads only world-readable files.
func CheckCommand() string {
	return checkVariables(cloudflaredBinaryPath, cloudflaredReceiptPath) + checkProvenance + checkServiceActive + checkSummary
}

// UnmanagedCheckCommand audits a connector on a host whose configured ingress
// is "none". That setting only says serverpro set up no tunnel; another tool
// can still install or run cloudflared, and such a connector carries the same
// trust-store risk as a managed one. The host decides its own state at read
// time: no package and no binary passes, an enabled or active service gets
// the full managed check, and a dormant package gets the floor and receipt
// check without a running service. Like CheckCommand it reads no 0600 file.
func UnmanagedCheckCommand() string {
	return unmanagedCheckCommand(cloudflaredBinaryPath, cloudflaredReceiptPath)
}

// unmanagedCheckCommand takes the on-disk paths so tests can execute the
// rendered check against a scratch binary and receipt.
func unmanagedCheckCommand(binaryPath, receiptPath string) string {
	return checkVariables(binaryPath, receiptPath) + hostStateHelper + unmanagedCheckDispatch + checkProvenance + unmanagedCheckService + checkSummary
}

// checkVariables pins every value the read checks use. The paths are
// parameters only so tests can point them at scratch files.
func checkVariables(binaryPath, receiptPath string) string {
	cloudflared := hostplatform.CloudflaredPackageBaseline()
	return fmt.Sprintf(`export LC_ALL=C
package_name=%s
minimum_version=%s
service_name=%s
receipt_path=%s
binary_path=%s
repository_url=%s
`,
		shell.Quote(cloudflared.Name),
		shell.Quote(cloudflared.MinimumVersion),
		shell.Quote(cloudflaredServiceName),
		shell.Quote(receiptPath),
		shell.Quote(binaryPath),
		shell.Quote(cloudflareAPTRepositoryURL),
	)
}

// checkProvenance is the floor and receipt gate both read checks share, so a
// managed and an unmanaged connector are held to one provenance standard.
const checkProvenance = `package_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' "${package_name}" 2>/dev/null) || package_record=
package_status=${package_record%%|*}
case "${package_record}" in
  installed'|'*) installed_version=${package_record#installed|} ;;
  ''|not-installed'|'*) printf '%s package is not installed\n' "${package_name}"; exit 1 ;;
  *) printf '%s package status is %s, want installed\n' "${package_name}" "${package_status}"; exit 1 ;;
esac
if ! dpkg --compare-versions "${installed_version}" ge "${minimum_version}"; then
  printf '%s %s is below minimum %s\n' "${package_name}" "${installed_version}" "${minimum_version}"
  exit 1
fi
receipt_version=
receipt_digest=
if [ -r "${receipt_path}" ]; then
  read -r receipt_version receipt_digest < "${receipt_path}"
fi
if [ -z "${receipt_version}" ]; then
  printf '%s install receipt is missing: %s\n' "${package_name}" "${receipt_path}"
  exit 1
fi
binary_digest=$(sha256sum "${binary_path}" 2>/dev/null | awk '{ print $1 }')
if [ -z "${binary_digest}" ] || [ "${receipt_version}" != "${installed_version}" ] || [ "${receipt_digest}" != "${binary_digest}" ]; then
  printf '%s %s is not the build serverpro installed from %s\n' "${package_name}" "${installed_version}" "${repository_url}"
  exit 1
fi
`

// checkServiceActive is the managed check's service gate: a tunnel that
// serverpro provisioned must always be serving.
const checkServiceActive = `if ! service_state=$(systemctl is-active "${service_name}" 2>&1); then
  printf '%s service is not active: %s\n' "${service_name}" "${service_state:-unknown}"
  exit 1
fi
`

// checkSummary is the one line a passing check prints; doctor shows it as
// evidence, so a repair visibly lands on the version and service state.
const checkSummary = `printf '%s %s %s\n' "${package_name}" "${installed_version}" "${service_state}"`

// unmanagedCheckDispatch classifies the host before any gate runs. An absent
// connector passes here; every other outcome falls through to the shared
// provenance gate, and an unclassifiable host fails with its reason.
const unmanagedCheckDispatch = `if ! host_state=$(cloudflared_host_state "${package_name}" "${service_name}" "${binary_path}"); then
  printf '%s\n' "${host_state}"
  exit 1
fi
case "${host_state}" in
  ` + hostStateAbsent + `) printf '%s not installed\n' "${package_name}"; exit 0 ;;
  ` + hostStateActive + `|` + hostStateDormant + `) ;;
  *) printf '%s host state is %s\n' "${package_name}" "${host_state}"; exit 1 ;;
esac
`

// unmanagedCheckService requires a running service only when someone enabled
// or started the connector. A dormant package must stay stopped, so its
// summary names the state instead of a systemd result.
const unmanagedCheckService = `service_state=` + hostStateDormant + `
if [ "${host_state}" = ` + hostStateActive + ` ]; then
  if ! service_state=$(systemctl is-active "${service_name}" 2>&1); then
    printf '%s service is not active: %s\n' "${service_name}" "${service_state:-unknown}"
    exit 1
  fi
fi
`

const (
	// hostStateAbsent, hostStateActive, and hostStateDormant are the only
	// states cloudflared_host_state prints; the read check and the repair
	// dispatch on them, so they must agree on the spelling.
	hostStateAbsent  = "absent"
	hostStateActive  = "active"
	hostStateDormant = "dormant"
)

// hostStateHelper is shared by the unmanaged read check and repair, so both
// decide from the live host and the repair never trusts a stale read. It
// fails closed: any dpkg, PATH, systemd, or process state outside the known
// absent/active/dormant shapes prints one reason and returns 1.
const hostStateHelper = `
# cloudflared_host_state prints absent, active, or dormant for the connector.
# Arguments: package name, systemd service name, packaged binary path.
# Absent means dpkg holds no installed package and no binary of that name is
# on PATH. Active means the service is enabled or running. Dormant means the
# package is present but the service is neither, and no connector process
# runs outside it, so a binary swap without a restart leaves nothing stale.
cloudflared_host_state() {
  host_package=$1
  host_service=$2
  host_binary=$3
  host_command=${host_binary##*/}
  host_present=
  if host_package_status=$(dpkg-query -W -f='${db:Status-Status}' "${host_package}" 2>/dev/null); then
    :
  else
    # dpkg-query exits 1 only when dpkg has no record of the package; any
    # other status is a query failure that must not read as absent.
    host_query_status=$?
    if [ "${host_query_status}" -ne 1 ]; then
      printf 'dpkg-query %s failed with status %s\n' "${host_package}" "${host_query_status}"
      return 1
    fi
    host_package_status=
  fi
  case "${host_package_status}" in
    installed) host_present=1 ;;
    # dpkg removed the files in both states, so only a binary on PATH can
    # still make the connector present.
    ''|not-installed|config-files) ;;
    *)
      printf '%s package status is %s, want installed or not-installed\n' "${host_package}" "${host_package_status}"
      return 1
      ;;
  esac
  # A binary elsewhere on PATH is not what the package floor, receipt, or
  # repair cover, so it is reported instead of counted as the package.
  if host_path=$(command -v "${host_command}" 2>/dev/null) && [ -n "${host_path}" ]; then
    host_path_resolved=$(readlink -f "${host_path}" 2>/dev/null) || host_path_resolved=${host_path}
    host_binary_resolved=$(readlink -f "${host_binary}" 2>/dev/null) || host_binary_resolved=${host_binary}
    if [ "${host_path_resolved}" != "${host_binary_resolved}" ]; then
      printf '%s on PATH is %s, not the packaged %s\n' "${host_command}" "${host_path}" "${host_binary}"
      return 1
    fi
    host_present=1
  fi
  if [ -z "${host_present}" ]; then
    printf '` + hostStateAbsent + `'
    return 0
  fi
  # systemctl show succeeds for a unit that does not exist (LoadState
  # not-found), so a failure here is a systemd problem, not an answer.
  if ! host_load=$(systemctl show --property=LoadState --value "${host_service}" 2>/dev/null) ||
    ! host_unit_file=$(systemctl show --property=UnitFileState --value "${host_service}" 2>/dev/null) ||
    ! host_active=$(systemctl show --property=ActiveState --value "${host_service}" 2>/dev/null); then
    printf 'systemctl show %s failed\n' "${host_service}"
    return 1
  fi
  case "${host_active}" in
    active|activating|reloading|refreshing|deactivating) host_running=1 ;;
    inactive|failed) host_running= ;;
    *)
      printf '%s service active state is %s, want a known systemd state\n' "${host_service}" "${host_active:-empty}"
      return 1
      ;;
  esac
  case "${host_load}:${host_unit_file}" in
    *:enabled|*:enabled-runtime) host_enabled=1 ;;
    *:disabled|*:masked|*:masked-runtime|not-found:) host_enabled= ;;
    *)
      printf '%s unit state is %s/%s, want enabled, disabled, masked, or not-found\n' "${host_service}" "${host_load:-empty}" "${host_unit_file:-empty}"
      return 1
      ;;
  esac
  if [ -n "${host_running}" ] || [ -n "${host_enabled}" ]; then
    printf '` + hostStateActive + `'
    return 0
  fi
  # pgrep exits 1 only when nothing matches; any other failure must not read
  # as "no connector running".
  if pgrep -x "${host_command}" >/dev/null 2>&1; then
    printf '%s is running outside %s.service\n' "${host_command}" "${host_service}"
    return 1
  else
    host_pgrep_status=$?
    if [ "${host_pgrep_status}" -ne 1 ]; then
      printf 'pgrep %s failed with status %s\n' "${host_command}" "${host_pgrep_status}"
      return 1
    fi
  fi
  printf '` + hostStateDormant + `'
}
`

// scriptHeader pins every value the shared helpers read, so install and
// repair agree on host, package floor, key fingerprint, and repository.
func scriptHeader() string {
	cloudflared := hostplatform.CloudflaredPackageBaseline()
	return fmt.Sprintf(`set -eu
umask 077
export LC_ALL=C

EXPECTED_HOST_OS=%s
EXPECTED_HOST_VERSION=%s
EXPECTED_HOST_CODENAME=%s
EXPECTED_HOST_ARCHITECTURES=%s
CLOUDFLARED_PACKAGE=%s
CLOUDFLARED_MINIMUM_VERSION=%s
CLOUDFLARED_SERVICE=%s
CLOUDFLARE_KEY_FINGERPRINT=%s
CLOUDFLARE_KEY_URL=%s
CLOUDFLARE_KEYRING=%s
CLOUDFLARED_APT_SOURCE_PATH=%s
CLOUDFLARED_APT_SOURCE=%s
CLOUDFLARED_LEGACY_APT_SOURCE=%s
CLOUDFLARED_APT_REPOSITORY_URL=%s
CLOUDFLARED_APT_PREFERENCES_PATH=%s
STAGING_SUFFIX=%s
DPKG_STATUS_PATH=%s
CLOUDFLARED_APT_PREFERENCES=%s
APT_LOCK_TIMEOUT=%d
CLOUDFLARED_UNIT=%s
CLOUDFLARED_TOKEN=%s
CLOUDFLARED_BINARY=%s
CLOUDFLARED_RECEIPT=%s
`,
		hostplatform.ManagedHostOS,
		hostplatform.ManagedHostVersion,
		hostplatform.ManagedHostCodename,
		shell.Quote(strings.Join(hostplatform.ManagedHostKernelArchitectures(), " ")),
		cloudflared.Name,
		cloudflared.MinimumVersion,
		cloudflaredServiceName,
		cloudflareAPTKeyFingerprint,
		cloudflareAPTKeyURL,
		cloudflareKeyringPath,
		cloudflaredAPTSourcePath,
		shell.Quote(cloudflaredAPTSource()),
		shell.Quote(cloudflaredLegacyAPTSource()),
		cloudflareAPTRepositoryURL,
		cloudflaredAPTPreferencesPath,
		stagingSuffix,
		dpkgStatusPath,
		shell.Quote(cloudflaredAPTPreferences()),
		hostplatform.APTLockTimeoutSeconds,
		cloudflaredUnitPath,
		cloudflaredTokenPath,
		cloudflaredBinaryPath,
		cloudflaredReceiptPath,
	)
}

// scriptHelpers holds the trust and version gates shared by install and
// repair. It contains no format verbs on purpose so shell printf strings stay
// readable without fmt escaping.
const scriptHelpers = `
cloudflare_key_tmp=
cloudflare_key_publish_tmp=
cleanup() {
  if [ -n "${cloudflare_key_tmp}" ]; then
    rm -f "${cloudflare_key_tmp}"
  fi
  if [ -n "${cloudflare_key_publish_tmp}" ]; then
    rm -f "${cloudflare_key_publish_tmp}"
  fi
}

require_supported_host() {
  . /etc/os-release
  if [ "${ID:-}" != "${EXPECTED_HOST_OS}" ] || [ "${VERSION_ID:-}" != "${EXPECTED_HOST_VERSION}" ] || [ "${VERSION_CODENAME:-}" != "${EXPECTED_HOST_CODENAME}" ]; then
    printf 'unsupported managed host: %s %s (%s)\n' "${ID:-unknown}" "${VERSION_ID:-unknown}" "${VERSION_CODENAME:-unknown}" >&2
    return 1
  fi
  case " ${EXPECTED_HOST_ARCHITECTURES} " in
    *" $(uname -m) "*) ;;
    *)
      printf 'unsupported managed-host architecture: %s\n' "$(uname -m)" >&2
      return 1
      ;;
  esac
}

installed_cloudflared_version() {
  package_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' "${CLOUDFLARED_PACKAGE}" 2>/dev/null) || return 1
  case "${package_record}" in
    installed'|'*) printf '%s' "${package_record#installed|}" ;;
    *) return 1 ;;
  esac
}

verify_cloudflared_minimum() {
  installed_version=$(installed_cloudflared_version) || {
    printf 'required package is not installed: %s\n' "${CLOUDFLARED_PACKAGE}" >&2
    return 1
  }
  if ! dpkg --compare-versions "${installed_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}"; then
    printf 'package %s is below minimum %s: %s\n' "${CLOUDFLARED_PACKAGE}" "${CLOUDFLARED_MINIMUM_VERSION}" "${installed_version}" >&2
    return 1
  fi
}

# apt_get waits for a concurrent apt or unattended-upgrades run instead of
# failing on the dpkg lock, and never prompts. Package-list refreshes go
# through apt_update because that lock is not covered here.
apt_get() {
  DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout="${APT_LOCK_TIMEOUT}" "$@"
}

# write_cloudflared_apt_pin makes apt prefer the Cloudflare origin. The pin
# ranks repositories but excludes none, so it cannot widen trust and the
# repair may (re)write it; verify_cloudflared_candidate is what refuses a
# version another repository offers.
write_cloudflared_apt_pin() {
  pin_staging_path=${CLOUDFLARED_APT_PREFERENCES_PATH}${STAGING_SUFFIX}
  install -d -m 0755 "$(dirname "${CLOUDFLARED_APT_PREFERENCES_PATH}")"
  printf '%s' "${CLOUDFLARED_APT_PREFERENCES}" > "${pin_staging_path}"
  chmod 0644 "${pin_staging_path}"
  mv -f "${pin_staging_path}" "${CLOUDFLARED_APT_PREFERENCES_PATH}"
}

# cloudflared_versions_from_trusted_origin prints each version that only the
# Cloudflare repository offers, so apt cannot fetch it from anywhere else. It
# is the enforcing origin control: the origin pin only ranks repositories.
#
# In the version table a version line is "[***] <version> <priority>" and each
# source under it is "<priority> <url> <suite> <arch> Packages" or
# "<priority> <dpkg status path>". The status line also has two fields, so it
# is matched before the version line or it would end the version being read.
# The status line is skipped because it records what is on disk, not a
# source. A version listed twice (a dpkg -i package whose control fields
# differ from Cloudflare's) is judged across both entries. Any other line
# under a version counts as an untrusted source, so an unexpected format
# fails closed. The policy is read before parsing because a pipeline would
# report awk's status and hide an apt-cache failure as "no trusted version".
cloudflared_versions_from_trusted_origin() {
  policy=$(apt-cache policy "${CLOUDFLARED_PACKAGE}") || return 1
  printf '%s\n' "${policy}" | awk -v repo="${CLOUDFLARED_APT_REPOSITORY_URL}" -v status="${DPKG_STATUS_PATH}" '
    /Version table:/ { table = 1; next }
    !table { next }
    {
      line = $0
      sub(/^[[:space:]]*[*][*][*][[:space:]]+/, "", line)
      n = split(line, field, " ")
      if (n == 0) { next }
      if (n == 2 && field[1] ~ /^-?[0-9]+$/ && field[2] == status) { next }
      if (n == 2 && field[2] ~ /^-?[0-9]+$/) { current = field[1]; next }
      if (current == "") { next }
      total[current]++
      if (field[1] ~ /^-?[0-9]+$/ && field[2] == repo && field[n] == "Packages") { trusted[current]++ }
    }
    END { for (version in total) if (total[version] == trusted[version]) print version }
  '
}

# newest_cloudflared_version_from_trusted_origin picks the install target
# instead of apt's Candidate: below priority 1000 apt keeps an installed newer
# connector from another source as Candidate, which would leave the repair
# nothing trusted to restore.
newest_cloudflared_version_from_trusted_origin() {
  newest=
  trusted_versions=$(cloudflared_versions_from_trusted_origin) || return 1
  for version in ${trusted_versions}; do
    if [ -z "${newest}" ] || ! dpkg --compare-versions "${newest}" ge "${version}"; then
      newest=${version}
    fi
  done
  printf '%s' "${newest}"
}

# cloudflared_binary_digest names the connector by content, because a
# same-version package from another source differs only in its bytes.
cloudflared_binary_digest() {
  digest=$(sha256sum "${CLOUDFLARED_BINARY}") || return 1
  printf '%s' "${digest%% *}"
}

# cloudflared_receipt_matches succeeds only when the installed connector is
# the exact build install_cloudflared_candidate recorded. Without it a
# connector at or above the floor would pass however it got onto the host.
cloudflared_receipt_matches() {
  installed_version=$(installed_cloudflared_version) || return 1
  [ -r "${CLOUDFLARED_RECEIPT}" ] || return 1
  read -r receipt_version receipt_digest < "${CLOUDFLARED_RECEIPT}" || return 1
  actual_digest=$(cloudflared_binary_digest 2>/dev/null) || return 1
  [ -n "${actual_digest}" ] && [ "${receipt_version}" = "${installed_version}" ] && [ "${receipt_digest}" = "${actual_digest}" ]
}

# record_cloudflared_receipt runs only after apt installed the verified
# Cloudflare version, so the receipt never vouches for another build.
record_cloudflared_receipt() {
  receipt_staging_path=${CLOUDFLARED_RECEIPT}${STAGING_SUFFIX}
  install -d -m 0755 "$(dirname "${CLOUDFLARED_RECEIPT}")"
  receipt_digest=$(cloudflared_binary_digest)
  printf '%s %s\n' "${candidate_version}" "${receipt_digest}" > "${receipt_staging_path}"
  chmod 0644 "${receipt_staging_path}"
  mv -f "${receipt_staging_path}" "${CLOUDFLARED_RECEIPT}"
}

# verify_cloudflared_candidate keeps a connector only when it is above the
# floor and matches the receipt. Anything else, including a newer connector
# from another source, is replaced by the newest version only Cloudflare
# offers.
verify_cloudflared_candidate() {
  candidate_version=
  if installed_version=$(installed_cloudflared_version) && dpkg --compare-versions "${installed_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}" && cloudflared_receipt_matches; then
    return 0
  fi
  if ! candidate_version=$(newest_cloudflared_version_from_trusted_origin); then
    printf 'apt-cache policy %s failed\n' "${CLOUDFLARED_PACKAGE}" >&2
    return 1
  fi
  if [ -z "${candidate_version}" ]; then
    printf 'no cloudflared version is offered only by %s\n' "${CLOUDFLARED_APT_REPOSITORY_URL}" >&2
    return 1
  fi
  if ! dpkg --compare-versions "${candidate_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}"; then
    printf 'cloudflared candidate is below minimum %s: %s\n' "${CLOUDFLARED_MINIMUM_VERSION}" "${candidate_version}" >&2
    return 1
  fi
}

# install_cloudflared_candidate installs the exact verified version and then
# records it. --reinstall replaces a same-version build from another source,
# and --allow-downgrades is added only to replace a newer untrusted connector,
# because the exact version is already verified as Cloudflare-only.
install_cloudflared_candidate() {
  if [ -z "${candidate_version:-}" ]; then
    return 0
  fi
  set -- --reinstall
  if installed_version=$(installed_cloudflared_version) && ! dpkg --compare-versions "${candidate_version}" ge "${installed_version}"; then
    set -- "$@" --allow-downgrades
  fi
  apt_get install -y "$@" "${CLOUDFLARED_PACKAGE}=${candidate_version}"
  installed_version=$(installed_cloudflared_version) || installed_version=none
  if [ "${installed_version}" != "${candidate_version}" ]; then
    printf 'cloudflared %s is installed after installing %s\n' "${installed_version}" "${candidate_version}" >&2
    return 1
  fi
  record_cloudflared_receipt
}

# Trust exactly one approved primary key; subkeys cannot satisfy identity and
# an appended primary key cannot hide behind the first fingerprint.
cloudflare_key_fingerprint() {
  gpg --show-keys --with-colons "$1" 2>/dev/null | awk -F: '
    $1 == "pub" { primary = 1; next }
    $1 == "sub" { primary = 0; next }
    $1 == "fpr" && primary { print $10; primary = 0 }
  ' | tr -d ' '
}

install_cloudflare_apt_key() {
  destination=$1
  expected=${CLOUDFLARE_KEY_FINGERPRINT}
  cloudflare_key_tmp=$(mktemp)
  curl -fsSL "${CLOUDFLARE_KEY_URL}" -o "${cloudflare_key_tmp}"
  actual=$(cloudflare_key_fingerprint "${cloudflare_key_tmp}")
  if [ "${actual}" != "${expected}" ]; then
    printf 'Cloudflare GPG key fingerprint mismatch: expected %s, got %s\n' "${expected}" "${actual:-none}" >&2
    rm -f "${cloudflare_key_tmp}"
    cloudflare_key_tmp=
    return 1
  fi
  cloudflare_key_publish_tmp=$(mktemp "${destination}.serverpro.XXXXXX")
  install -m 0644 "${cloudflare_key_tmp}" "${cloudflare_key_publish_tmp}"
  mv -f "${cloudflare_key_publish_tmp}" "${destination}"
  cloudflare_key_publish_tmp=
  rm -f "${cloudflare_key_tmp}"
  cloudflare_key_tmp=
}

# Repair trusts only what provisioning already pinned: it re-verifies the
# installed keyring instead of fetching a new key, so a repair can never widen
# trust on a host whose keyring was removed or replaced.
verify_trusted_cloudflare_apt_key() {
  if [ ! -s "${CLOUDFLARE_KEYRING}" ]; then
    printf 'trusted Cloudflare apt key is missing: %s; reprovision the server\n' "${CLOUDFLARE_KEYRING}" >&2
    return 1
  fi
  actual=$(cloudflare_key_fingerprint "${CLOUDFLARE_KEYRING}")
  if [ "${actual}" != "${CLOUDFLARE_KEY_FINGERPRINT}" ]; then
    printf 'Cloudflare GPG key fingerprint mismatch: expected %s, got %s\n' "${CLOUDFLARE_KEY_FINGERPRINT}" "${actual:-none}" >&2
    return 1
  fi
}

verify_cloudflared_apt_source() {
  actual_source=$(cat "${CLOUDFLARED_APT_SOURCE_PATH}" 2>/dev/null) || actual_source=
  # Only the exact legacy line is migrated: it trusts the same keyring and
  # repository, so the rewrite cannot widen trust. Any other content still
  # stops the repair.
  if [ "${actual_source}" = "${CLOUDFLARED_LEGACY_APT_SOURCE}" ]; then
    source_staging_path=${CLOUDFLARED_APT_SOURCE_PATH}${STAGING_SUFFIX}
    printf '%s\n' "${CLOUDFLARED_APT_SOURCE}" > "${source_staging_path}"
    chmod 0644 "${source_staging_path}"
    mv -f "${source_staging_path}" "${CLOUDFLARED_APT_SOURCE_PATH}"
    actual_source=$(cat "${CLOUDFLARED_APT_SOURCE_PATH}")
  fi
  if [ "${actual_source}" != "${CLOUDFLARED_APT_SOURCE}" ]; then
    printf 'cloudflared apt source is missing or changed: %s; reprovision the server\n' "${CLOUDFLARED_APT_SOURCE_PATH}" >&2
    return 1
  fi
}

# Repair restarts the connector but never rewrites its token or unit; those
# come only from provisioning, so their absence must stop the repair.
require_cloudflared_service_setup() {
  if [ ! -f "${CLOUDFLARED_UNIT}" ] || [ ! -s "${CLOUDFLARED_TOKEN}" ]; then
    printf 'cloudflared tunnel service is not set up (%s, %s); reprovision the server\n' "${CLOUDFLARED_UNIT}" "${CLOUDFLARED_TOKEN}" >&2
    return 1
  fi
}
`

// scriptPreamble joins the pinned values, the connector helpers, and the
// host-wide apt_update so install and repair refresh package lists with the
// same lists-lock wait as every other managed-host script.
func scriptPreamble() string {
	return scriptHeader() + scriptHelpers + "\n" + hostplatform.APTUpdateShellFunction()
}

// scriptFooter keeps sourcing side-effect free.
const scriptFooter = `
# The guard lets tests execute trust helpers without running privileged setup.
if [ -z "${SERVERPRO_TUNNEL_SOURCE_ONLY:-}" ]; then
  main
fi
`

// InstallScript is the provisioning-time setup: it pins the Cloudflare apt
// key, installs the connector at or above the floor, and writes the token and
// unit that the doctor repair later relies on but never rewrites.
func InstallScript(token string) string {
	return scriptPreamble() + fmt.Sprintf(`
main() {
  require_supported_host
  trap cleanup EXIT
  install -d -m 0755 %[1]s %[2]s
  install -d -m 0700 %[3]s
  install_cloudflare_apt_key %[4]s
  printf '%%s\n' "${CLOUDFLARED_APT_SOURCE}" > %[5]s
  chmod 0644 %[5]s
  write_cloudflared_apt_pin
  apt_update
  verify_cloudflared_candidate
  install_cloudflared_candidate
  verify_cloudflared_minimum
  id -u %[6]s >/dev/null 2>&1 || useradd --system --home %[3]s --shell /usr/sbin/nologin %[6]s
  printf '%%s' %[7]s > %[8]s
  chown -R %[6]s:%[6]s %[3]s
  chmod 0700 %[3]s
  chmod 0600 %[8]s
  cat > %[9]s <<'UNIT'
[Unit]
Description=Cloudflare Tunnel connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%[6]s
Group=%[6]s
ExecStart=%[10]s --no-autoupdate tunnel run --token-file %[8]s
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now %[11]s
  systemctl is-active %[11]s
}
`,
		path.Dir(cloudflareKeyringPath),
		path.Dir(cloudflaredAPTSourcePath),
		CloudflaredConfigDir,
		cloudflareKeyringPath,
		cloudflaredAPTSourcePath,
		cloudflaredServiceUser,
		shell.Quote(token),
		cloudflaredTokenPath,
		cloudflaredUnitPath,
		cloudflaredBinaryPath,
		cloudflaredServiceName,
	) + scriptFooter
}

// trustedReinstallSteps reinstalls the connector from the trusted Cloudflare
// repository when it is below the floor or does not match its receipt. It
// reuses only the keyring and source provisioning pinned, so it can never
// widen trust, and it touches neither the token nor the service. Both repairs
// share it so a dormant connector is held to the managed trust gates.
const trustedReinstallSteps = `  if ! verify_cloudflared_minimum || ! cloudflared_receipt_matches; then
    verify_trusted_cloudflare_apt_key
    verify_cloudflared_apt_source
    write_cloudflared_apt_pin
    apt_update
    verify_cloudflared_candidate
    install_cloudflared_candidate
    verify_cloudflared_minimum
  fi
`

// serviceRepairSteps is the repair for a connector that is meant to run: it
// needs the provisioned unit and token, reinstalls when needed, and restarts
// so the running process loads the new binary.
const serviceRepairSteps = `  require_cloudflared_service_setup
` + trustedReinstallSteps + `  # Restart even when enabled so the running connector loads the new binary.
  systemctl enable "${CLOUDFLARED_SERVICE}"
  systemctl restart "${CLOUDFLARED_SERVICE}"
  systemctl is-active "${CLOUDFLARED_SERVICE}"
`

// UpgradeScript is the doctor repair for an outdated, untrusted, or stopped
// connector. A connector that does not match the install receipt is
// reinstalled from Cloudflare, so a dpkg -i or other-repository build is
// replaced even when it is newer.
// Provisioning pins cloudflared with --no-autoupdate and stock unattended
// upgrades skip pkg.cloudflare.com, so a raised package floor otherwise never
// reaches existing hosts. It reuses provisioning's trusted repository and
// never touches the tunnel token, so it cannot rotate or leak credentials.
func UpgradeScript() string {
	return scriptPreamble() + `
main() {
  require_supported_host
` + serviceRepairSteps + `}
` + scriptFooter
}

// UnmanagedRepairScript is the doctor repair paired with
// UnmanagedCheckCommand. It reclassifies the host itself instead of trusting
// the earlier read: an active connector gets the managed repair, so it still
// needs the provisioned unit and token; a dormant one is only reinstalled
// from the trusted repository and verified to still be dormant, because
// starting a connector nobody runs would open ingress serverpro was told not
// to have. An absent connector is left alone.
func UnmanagedRepairScript() string {
	return scriptPreamble() + hostStateHelper + `
repair_active_cloudflared() {
` + serviceRepairSteps + `}

repair_dormant_cloudflared() {
` + trustedReinstallSteps + `  # The package scripts must not have enabled or started the connector.
  if ! host_state=$(cloudflared_host_state "${CLOUDFLARED_PACKAGE}" "${CLOUDFLARED_SERVICE}" "${CLOUDFLARED_BINARY}") || [ "${host_state}" != ` + hostStateDormant + ` ]; then
    printf '%s is no longer dormant after the reinstall: %s\n' "${CLOUDFLARED_PACKAGE}" "${host_state}" >&2
    return 1
  fi
  printf '%s %s %s\n' "${CLOUDFLARED_PACKAGE}" "$(installed_cloudflared_version)" "${host_state}"
}

main() {
  require_supported_host
  if ! host_state=$(cloudflared_host_state "${CLOUDFLARED_PACKAGE}" "${CLOUDFLARED_SERVICE}" "${CLOUDFLARED_BINARY}"); then
    printf '%s\n' "${host_state}" >&2
    return 1
  fi
  case "${host_state}" in
    ` + hostStateAbsent + `) printf '%s not installed\n' "${CLOUDFLARED_PACKAGE}" ;;
    ` + hostStateActive + `) repair_active_cloudflared ;;
    ` + hostStateDormant + `) repair_dormant_cloudflared ;;
    *)
      printf '%s host state is %s\n' "${CLOUDFLARED_PACKAGE}" "${host_state}" >&2
      return 1
      ;;
  esac
}
` + scriptFooter
}
