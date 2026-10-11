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
	cloudflaredConfigDir   = "/etc/cloudflared"
	cloudflaredTokenPath   = cloudflaredConfigDir + "/token"
	cloudflaredUnitPath    = "/etc/systemd/system/cloudflared.service"
	cloudflaredBinaryPath  = "/usr/bin/cloudflared"
)

// cloudflaredAPTSource is the exact source line provisioning writes. The
// repair compares against it so a hand-edited or signed-by-less source can
// never be treated as the trusted repository.
func cloudflaredAPTSource() string {
	return "deb [signed-by=" + cloudflareKeyringPath + "] " + cloudflareAPTRepositoryURL + " " + hostplatform.ManagedHostCodename + " " + cloudflareAPTComponent
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
	cloudflared := hostplatform.CloudflaredPackageBaseline()
	return fmt.Sprintf(`export LC_ALL=C
package_name=%[1]s
minimum_version=%[2]s
service_name=%[3]s
receipt_path=%[4]s
binary_path=%[5]s
repository_url=%[6]s
package_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' "${package_name}" 2>/dev/null) || package_record=
package_status=${package_record%%%%|*}
case "${package_record}" in
  installed'|'*) installed_version=${package_record#installed|} ;;
  ''|not-installed'|'*) printf '%%s package is not installed\n' "${package_name}"; exit 1 ;;
  *) printf '%%s package status is %%s, want installed\n' "${package_name}" "${package_status}"; exit 1 ;;
esac
if ! dpkg --compare-versions "${installed_version}" ge "${minimum_version}"; then
  printf '%%s %%s is below minimum %%s\n' "${package_name}" "${installed_version}" "${minimum_version}"
  exit 1
fi
receipt_version=
receipt_digest=
if [ -r "${receipt_path}" ]; then
  read -r receipt_version receipt_digest < "${receipt_path}"
fi
if [ -z "${receipt_version}" ]; then
  printf '%%s install receipt is missing: %%s\n' "${package_name}" "${receipt_path}"
  exit 1
fi
binary_digest=$(sha256sum "${binary_path}" 2>/dev/null | awk '{ print $1 }')
if [ -z "${binary_digest}" ] || [ "${receipt_version}" != "${installed_version}" ] || [ "${receipt_digest}" != "${binary_digest}" ]; then
  printf '%%s %%s is not the build serverpro installed from %%s\n' "${package_name}" "${installed_version}" "${repository_url}"
  exit 1
fi
if ! service_state=$(systemctl is-active "${service_name}" 2>&1); then
  printf '%%s service is not active: %%s\n' "${service_name}" "${service_state:-unknown}"
  exit 1
fi
printf '%%s %%s %%s\n' "${package_name}" "${installed_version}" "${service_state}"`,
		shell.Quote(cloudflared.Name),
		shell.Quote(cloudflared.MinimumVersion),
		shell.Quote(cloudflaredServiceName),
		shell.Quote(cloudflaredReceiptPath),
		shell.Quote(cloudflaredBinaryPath),
		shell.Quote(cloudflareAPTRepositoryURL),
	)
}

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
		cloudflaredConfigDir,
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
  require_cloudflared_service_setup
  if ! verify_cloudflared_minimum || ! cloudflared_receipt_matches; then
    verify_trusted_cloudflare_apt_key
    verify_cloudflared_apt_source
    write_cloudflared_apt_pin
    apt_update
    verify_cloudflared_candidate
    install_cloudflared_candidate
    verify_cloudflared_minimum
  fi
  # Restart even when enabled so the running connector loads the new binary.
  systemctl enable "${CLOUDFLARED_SERVICE}"
  systemctl restart "${CLOUDFLARED_SERVICE}"
  systemctl is-active "${CLOUDFLARED_SERVICE}"
}
` + scriptFooter
}
