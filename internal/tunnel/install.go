// Package tunnel owns the Cloudflare Tunnel connector contract on managed
// hosts: the provisioning install, the doctor check, and the doctor repair all
// render from one set of shell helpers so trust pinning and the package floor
// cannot drift between them.
package tunnel

import (
	"fmt"
	"path"
	"strings"

	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	// CheckName is the doctor result name; doctor evidence summaries and
	// specifications key on it, so it lives next to the check it names.
	CheckName = "cloudflared"

	cloudflareAPTKeyFingerprint = "CC94B39C77AE7342A68B89628A682D308D4E5E73"
	cloudflareAPTKeyURL         = "https://pkg.cloudflare.com/cloudflare-main.gpg"
	cloudflareAPTRepositoryURL  = "https://pkg.cloudflare.com/cloudflared"
	cloudflareAPTComponent      = "main"
	cloudflareKeyringPath       = "/usr/share/keyrings/cloudflare-main.gpg"
	cloudflaredAPTSourcePath    = "/etc/apt/sources.list.d/cloudflared.list"
	cloudflaredServiceName      = "cloudflared"
	cloudflaredServiceUser      = "cloudflared"
	cloudflaredConfigDir        = "/etc/cloudflared"
	cloudflaredTokenPath        = cloudflaredConfigDir + "/token"
	cloudflaredUnitPath         = "/etc/systemd/system/cloudflared.service"
	cloudflaredBinaryPath       = "/usr/bin/cloudflared"
)

// cloudflaredAPTSource is the exact source line provisioning writes. The
// repair compares against it so a hand-edited or signed-by-less source can
// never be treated as the trusted repository.
func cloudflaredAPTSource() string {
	return "deb [signed-by=" + cloudflareKeyringPath + "] " + cloudflareAPTRepositoryURL + " " + hostplatform.ManagedHostCodename + " " + cloudflareAPTComponent
}

// CheckCommand prints exactly one reason when the connector is unhealthy.
// Doctor evidence carries this output because the batch error alone only
// reports an exit status, which cannot tell a missing package from an
// outdated one or a stopped service.
func CheckCommand() string {
	cloudflared := hostplatform.CloudflaredPackageBaseline()
	return fmt.Sprintf(`export LC_ALL=C
package_name=%[1]s
minimum_version=%[2]s
service_name=%[3]s
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
if ! service_state=$(systemctl is-active "${service_name}" 2>&1); then
  printf '%%s service is not active: %%s\n' "${service_name}" "${service_state:-unknown}"
  exit 1
fi
printf '%%s %%s %%s\n' "${package_name}" "${installed_version}" "${service_state}"`,
		shell.Quote(cloudflared.Name),
		shell.Quote(cloudflared.MinimumVersion),
		shell.Quote(cloudflaredServiceName),
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
CLOUDFLARED_UNIT=%s
CLOUDFLARED_TOKEN=%s
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
		cloudflaredUnitPath,
		cloudflaredTokenPath,
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

verify_cloudflared_candidate() {
  if installed_version=$(installed_cloudflared_version) && dpkg --compare-versions "${installed_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}"; then
    return 0
  fi
  candidate_version=$(apt-cache policy "${CLOUDFLARED_PACKAGE}" | awk '$1 == "Candidate:" { print $2; exit }')
  if [ -z "${candidate_version}" ] || [ "${candidate_version}" = '(none)' ]; then
    printf 'cloudflared has no install candidate\n' >&2
    return 1
  fi
  if ! dpkg --compare-versions "${candidate_version}" ge "${CLOUDFLARED_MINIMUM_VERSION}"; then
    printf 'cloudflared candidate is below minimum %s: %s\n' "${CLOUDFLARED_MINIMUM_VERSION}" "${candidate_version}" >&2
    return 1
  fi
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
	return scriptHeader() + scriptHelpers + fmt.Sprintf(`
main() {
  require_supported_host
  trap cleanup EXIT
  install -d -m 0755 %[1]s %[2]s
  install -d -m 0700 %[3]s
  install_cloudflare_apt_key %[4]s
  printf '%%s\n' "${CLOUDFLARED_APT_SOURCE}" > %[5]s
  chmod 0644 %[5]s
  apt-get update
  verify_cloudflared_candidate
  apt-get install -y "${CLOUDFLARED_PACKAGE}"
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

// UpgradeScript is the doctor repair for an outdated or stopped connector.
// Provisioning pins cloudflared with --no-autoupdate and stock unattended
// upgrades skip pkg.cloudflare.com, so a raised package floor otherwise never
// reaches existing hosts. It reuses provisioning's trusted repository and
// never touches the tunnel token, so it cannot rotate or leak credentials.
func UpgradeScript() string {
	return scriptHeader() + scriptHelpers + `
main() {
  require_supported_host
  require_cloudflared_service_setup
  if ! verify_cloudflared_minimum; then
    verify_trusted_cloudflare_apt_key
    verify_cloudflared_apt_source
    DEBIAN_FRONTEND=noninteractive apt-get update
    verify_cloudflared_candidate
    DEBIAN_FRONTEND=noninteractive apt-get install -y "${CLOUDFLARED_PACKAGE}"
    verify_cloudflared_minimum
  fi
  # Restart even when enabled so the running connector loads the new binary.
  systemctl enable "${CLOUDFLARED_SERVICE}"
  systemctl restart "${CLOUDFLARED_SERVICE}"
  systemctl is-active "${CLOUDFLARED_SERVICE}"
}
` + scriptFooter
}
