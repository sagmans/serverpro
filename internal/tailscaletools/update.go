package tailscaletools

import (
	_ "embed"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	Version      = "1.102.3"
	AMD64SHA256  = "36ddd9b51be57ffc2990cf76323cfa13643bfbb1b8a969f6183fa164741cdef5"
	ARM64SHA256  = "a0fa1b154af8c61f862a2259f559f7396d96c0225f4a863eae2333e1546bbe25"
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
