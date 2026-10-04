// Buildx fallback identity stays shared so provisioning and diagnosis cannot
// disagree about which Docker plugin is safe to execute.
package bootstraptools

import (
	"github.com/sagmans/serverpro/internal/hostplatform"
	"strings"
)

const (
	BuildxVersion          = "0.37.2"
	BuildxLinuxAMD64SHA256 = "982ca20490b45ed1ec8d99795974d3d874a358f75938c9c237305010e6b7e548"
	BuildxLinuxARM64SHA256 = "efa38cb7aa7db2dbb9ad049b00b0a9737f66f033626177b5a4e845184ad7ab29"
	BuildxPackage          = "docker-buildx-plugin"
	BuildxManagedDirectory = "/usr/local/lib/serverpro/docker-buildx"
	BuildxPluginPath       = "/usr/local/lib/docker/cli-plugins/docker-buildx"
)

// buildxProbe rejects shadowing before Docker can execute an unverified plugin.
const buildxProbe = `set -e
buildx_source=
buildx_package_minimum='__PACKAGE_MINIMUM__'
buildx_selected=
buildx_config="${DOCKER_CONFIG:-$HOME/.docker}"
buildx_extra=
if test -f "$buildx_config/config.json"; then
  buildx_extra=$(jq -r '(.cliPluginsExtraDirs // [])[]' "$buildx_config/config.json")
fi
while IFS= read -r buildx_dir; do
  test -n "$buildx_dir" || continue
  if test -f "$buildx_dir/docker-buildx" || test -L "$buildx_dir/docker-buildx"; then
    buildx_selected="$buildx_dir/docker-buildx"
    break
  fi
done <<BUILDX_DIRS
$buildx_extra
$buildx_config/cli-plugins
/usr/local/lib/docker/cli-plugins
/usr/local/libexec/docker/cli-plugins
/usr/lib/docker/cli-plugins
/usr/libexec/docker/cli-plugins
BUILDX_DIRS
case "$(uname -m)" in
  x86_64) buildx_arch=amd64; buildx_sha=__AMD64_SHA__ ;;
  aarch64|arm64) buildx_arch=arm64; buildx_sha=__ARM64_SHA__ ;;
  *) printf 'unsupported Buildx architecture\n' >&2; exit 1 ;;
esac
if test "$buildx_selected" = '__PLUGIN_PATH__'; then
  for buildx_parent in /usr /usr/local /usr/local/lib /usr/local/lib/serverpro /usr/local/lib/serverpro/docker-buildx /usr/local/lib/docker /usr/local/lib/docker/cli-plugins; do
    test ! -L "$buildx_parent" && test "$(stat -c %u "$buildx_parent")" = 0 || { printf 'unsafe managed Buildx directory\n' >&2; exit 1; }
    buildx_mode=$(stat -c %a "$buildx_parent")
    test $((0$buildx_mode & 022)) = 0 || { printf 'writable managed Buildx directory\n' >&2; exit 1; }
  done
  test "$(stat -c %u "$buildx_selected")" = 0 || { printf 'unsafe managed Buildx alias\n' >&2; exit 1; }
  buildx_artifact='__MANAGED_DIR__/__VERSION__-'"$buildx_arch"
  test -L "$buildx_selected" && test "$(readlink "$buildx_selected")" = "$buildx_artifact" || { printf 'unmanaged Buildx override: %s\n' "$buildx_selected" >&2; exit 1; }
  test ! -L "$buildx_artifact" && test -f "$buildx_artifact" && test "$(stat -c %u "$buildx_artifact")" = 0 && test "$(stat -c %a "$buildx_artifact")" = 755 || { printf 'unsafe managed Buildx artifact\n' >&2; exit 1; }
  test "$(sha256sum "$buildx_artifact" | awk '{print $1}')" = "$buildx_sha" || { printf 'managed Buildx SHA-256 mismatch\n' >&2; exit 1; }
  buildx_source=managed
else
  case "$buildx_selected" in
    /usr/local/libexec/docker/cli-plugins/docker-buildx|/usr/lib/docker/cli-plugins/docker-buildx|/usr/libexec/docker/cli-plugins/docker-buildx) ;;
    *) printf 'untrusted Buildx plugin path: %s\n' "$buildx_selected" >&2; exit 1 ;;
  esac
  test ! -L "$buildx_selected" && test "$(stat -c %u "$buildx_selected")" = 0 && test "$(stat -c %a "$buildx_selected")" = 755 || { printf 'unsafe packaged Buildx plugin\n' >&2; exit 1; }
  dpkg-query -L __PACKAGE__ | grep -Fxq "$buildx_selected" || { printf 'Buildx plugin is not package-owned\n' >&2; exit 1; }
  buildx_record=$(dpkg-query -W -f='${db:Status-Status}|${Version}' __PACKAGE__)
  case "$buildx_record" in installed'|'*) ;; *) exit 1 ;; esac
  dpkg --compare-versions "${buildx_record#installed|}" ge '__PACKAGE_MINIMUM__' || { printf 'packaged Buildx below security baseline\n' >&2; exit 1; }
  buildx_source=apt
fi
buildx_output=$(docker buildx version)
buildx_current=$(printf '%s\n' "$buildx_output" | awk '{print $2}')
buildx_current=${buildx_current#v}
dpkg --compare-versions "$buildx_current" ge '__VERSION__' || { printf 'loaded Buildx below security baseline: %s\n' "$buildx_output" >&2; exit 1; }
printf '%s\n' "$buildx_output"
`

// buildxCheckCommand binds the same reviewed identity into bootstrap and doctor.
func buildxCheckCommand() string {
	minimum := ""
	for _, pkg := range hostplatform.DockerPackageBaselines() {
		if pkg.Name == BuildxPackage {
			minimum = pkg.MinimumVersion
		}
	}
	return strings.TrimSpace(strings.NewReplacer(
		"__VERSION__", BuildxVersion,
		"__AMD64_SHA__", BuildxLinuxAMD64SHA256,
		"__ARM64_SHA__", BuildxLinuxARM64SHA256,
		"__PLUGIN_PATH__", BuildxPluginPath,
		"__MANAGED_DIR__", BuildxManagedDirectory,
		"__PACKAGE__", BuildxPackage,
		"__PACKAGE_MINIMUM__", minimum,
	).Replace(buildxProbe))
}
