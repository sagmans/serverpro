package tailscaletools

import (
	_ "embed"
	"strconv"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	// Older releases let Tailscale SSH forward to root-only unix sockets
	// (TS-2026-014), allow an SSH memory DoS (TS-2026-015), carry the Serve
	// flaws TS-2026-016/017, and lack the TS-2026-013 eMudhra distrust. Hosts
	// already on 1.104.x must also never be pinned back to an older release.
	Version      = "1.104.4"
	AMD64SHA256  = "624ea093d47d62cf56f17b8332d74b8decda4a1faa610ebc60a1de334b2b3dc9"
	ARM64SHA256  = "a1c249ce6bdebef62e1ae3b997595555bce2ae4709bf95a718e9ca7a129dad7e"
	CheckName    = "tailscale " + Version
	RestartGrace = 5 * time.Second
	restartDelay = "2s"
)

//go:embed update.sh
var updateScript string

func CheckCommand() string {
	return `client=$(tailscale version --json | jq -r '.short // empty'); daemon=$(tailscale status --json | jq -r '.Version // empty'); test "$client" = "` + Version + `" || { printf 'expected Tailscale client ` + Version + `, got %s\n' "$client" >&2; exit 1; }; case "$daemon" in "` + Version + `"|"` + Version + `"-*) ;; *) printf 'expected Tailscale daemon ` + Version + `, got %s\n' "$daemon" >&2; exit 1 ;; esac; systemctl is-active tailscaled; printf 'client=%s daemon=%s\n' "$client" "$daemon"`
}

func UpdateScript() string {
	values := [][2]string{
		{"SERVERPRO_TAILSCALE_HOST_OS", hostplatform.ManagedHostOS},
		{"SERVERPRO_TAILSCALE_HOST_VERSION", hostplatform.ManagedHostVersion},
		{"SERVERPRO_TAILSCALE_HOST_CODENAME", hostplatform.ManagedHostCodename},
		{"SERVERPRO_TAILSCALE_HOST_ARCHITECTURES", strings.Join(hostplatform.ManagedHostKernelArchitectures(), " ")},
		{"SERVERPRO_TAILSCALE_PACKAGE_BASELINES", hostplatform.PackageBaselineManifest(hostplatform.TailscalePrerequisitePackageBaselines())},
		{"SERVERPRO_TAILSCALE_PACKAGES", strings.Join(hostplatform.PackageNames(hostplatform.TailscalePrerequisitePackageBaselines()), " ")},
		{"SERVERPRO_TAILSCALE_APT_LOCK_TIMEOUT", strconv.Itoa(hostplatform.APTLockTimeoutSeconds)},
		{"SERVERPRO_TAILSCALE_APT_LISTS_LOCK_RETRY_SECONDS", strconv.Itoa(hostplatform.APTListsLockRetrySeconds)},
		{"SERVERPRO_TAILSCALE_APT_LISTS_LOCK_ERROR", hostplatform.APTListsLockError},
		{"SERVERPRO_TAILSCALE_VERSION", Version},
		{"SERVERPRO_TAILSCALE_SHA256_AMD64", AMD64SHA256},
		{"SERVERPRO_TAILSCALE_SHA256_ARM64", ARM64SHA256},
		{"SERVERPRO_TAILSCALE_RESTART_DELAY", restartDelay},
	}
	var b strings.Builder
	for _, value := range values {
		b.WriteString(value[0])
		b.WriteByte('=')
		b.WriteString(shell.Quote(value[1]))
		b.WriteByte('\n')
	}
	exports := make([]string, len(values))
	for i, value := range values {
		exports[i] = value[0]
	}
	b.WriteString("export ")
	b.WriteString(strings.Join(exports, " "))
	b.WriteByte('\n')
	b.WriteString(updateScript)
	return b.String()
}
