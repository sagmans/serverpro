package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
)

func remoteInventory(ctx context.Context, r remote.Runner, user, host string) []InventoryItem {
	if r == nil || host == "" {
		return nil
	}
	out, err := r.Run(ctx, user, host, remoteInventoryCommand())
	if err != nil {
		return nil
	}
	return []InventoryItem{{Scope: "remote", Name: "host", Value: trim(out)}}
}

func remoteInventoryCommand() string {
	return `os="$([ -r /etc/os-release ] && . /etc/os-release && printf '%s %s' "$NAME" "$VERSION_ID" || uname -s)"
kernel="$(uname -r)"
cpu="$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN)"
ram_kib="$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || true)"
printf 'os=%s kernel=%s cpu=%s ram_kib=%s' "$os" "$kernel" "$cpu" "$ram_kib"`
}

const (
	cloudInitWaitCommand = "cloud-init status --wait"
	cloudInitLongCommand = "cloud-init status --long"
)

func remoteCloudInitCheck(ctx context.Context, r remote.Runner, user, host, logPath string) Result {
	out, err := r.Run(ctx, user, host, cloudInitWaitCommand)
	if err == nil {
		return pass("remote", "cloud-init", summarizeRemoteEvidence("cloud-init", out))
	}
	evidence := err.Error()
	statusEvidence := strings.ToLower(evidence + "\n" + out)
	exitStatusTwo := strings.Contains(evidence, "exit status 2") || strings.HasSuffix(evidence, "failed with status 2")
	if exitStatusTwo && strings.Contains(statusEvidence, "status: done") {
		remediation := "inspect /var/log/cloud-init.log and /var/log/cloud-init-output.log"
		if detail, _ := r.Run(ctx, user, host, cloudInitLongCommand); strings.TrimSpace(detail) != "" {
			if logPath != "" {
				if err := writeDoctorLog(logPath, detail); err == nil {
					remediation += "; full status saved: " + logPath
				} else {
					remediation += "; failed to save full status: " + err.Error()
				}
			}
			evidence = "status: done with recoverable cloud-init warnings; " + trim(detail)
		} else {
			evidence = "status: done with recoverable cloud-init warnings"
		}
		return Result{Name: "cloud-init", Scope: "remote", Status: Warn, Evidence: trim(evidence), Remediation: remediation}
	}
	return fail("remote", "cloud-init", evidence, "inspect remote command")
}

func cloudInitStatusLogPath(cfg config.Config) string {
	server := cfg.Server
	if server == "" {
		server = config.DefaultServer()
	}
	return config.Expand(filepath.Join("~/.local/share/serverpro/logs", cfg.Namespace, server, "cloud-init-status-long.txt"))
}

func writeDoctorLog(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}
