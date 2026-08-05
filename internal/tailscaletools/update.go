package tailscaletools

import (
	_ "embed"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/shell"
)

const (
	Version      = "1.98.10"
	AMD64SHA256  = "52490ce0832b245857e2afef7426d6ae5a4b49fb391412833cc95729bd23f7de"
	ARM64SHA256  = "d74a84e07cb1948d9f09a23ae161417c6127e562949773705c95d0762be2809d"
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
	b.WriteString("export SERVERPRO_TAILSCALE_VERSION SERVERPRO_TAILSCALE_SHA256_AMD64 SERVERPRO_TAILSCALE_SHA256_ARM64 SERVERPRO_TAILSCALE_RESTART_DELAY\n")
	b.WriteString(updateScript)
	return b.String()
}
