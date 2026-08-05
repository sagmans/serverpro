package doctor

import (
	"context"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/remote"
	"github.com/assagman/serverpro/internal/shell"
)

func remoteChecksWithOptions(ctx context.Context, cfg config.Config, r remote.Runner, host string, opt Options) []Result {
	if r == nil || host == "" {
		return nil
	}
	user := cfg.Admin.Username
	results := []Result{
		remoteSudoPasswordRequiredCheck(ctx, r, user, host, opt),
		remoteCloudInitCheck(ctx, r, user, host, cloudInitStatusLogPath(cfg)),
		remoteCheck(ctx, r, user, host, "admin user", "id "+shell.Quote(user)),
		remoteFixableCheck(ctx, r, user, host, "ufw active", "ufw status verbose | grep -Fx 'Status: active'", "ufw --force enable", opt),
		remoteFixableCheck(ctx, r, user, host, "ufw default deny incoming", "ufw status verbose | grep -F 'Default: deny (incoming)'", "ufw default deny incoming && ufw --force reload", opt),
		remoteFixableCheck(ctx, r, user, host, "ufw ssh ingress", ufwSSHIngressCommand(), "ufw delete allow OpenSSH || true\nufw delete allow 22/tcp || true\nufw delete allow 22 || true\nufw --force reload", opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd root login", sshdKeywordPermitRootLogin, opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd password auth", sshdKeywordPasswordAuthentication, opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd keyboard-interactive auth", sshdKeywordKbdInteractiveAuthentication, opt),
		remoteSSHDChallengeResponseCheckWithOptions(ctx, r, user, host, opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd x11 forwarding", sshdKeywordX11Forwarding, opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd agent forwarding", sshdKeywordAllowAgentForwarding, opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd tcp forwarding", sshdKeywordAllowTCPForwarding, opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd tunnel forwarding", sshdKeywordPermitTunnel, opt),
		remoteSSHDSettingCheckWithOptions(ctx, r, user, host, "sshd open forwarding", sshdKeywordPermitOpen, opt),
		remoteFixableCheck(ctx, r, user, host, "apparmor", "systemctl is-active apparmor && aa-status --enabled", "systemctl enable --now apparmor", opt),
		remoteFixableCheck(ctx, r, user, host, "unattended upgrades", "systemctl is-enabled unattended-upgrades || systemctl is-enabled apt-daily.timer", "systemctl enable --now unattended-upgrades || systemctl enable --now apt-daily.timer", opt),
		remoteFixableCheck(ctx, r, user, host, "journald persistent", "test -d /var/log/journal", "mkdir -p /var/log/journal && systemctl restart systemd-journald", opt),
	}
	results = append(results, remoteToolChecks(ctx, r, user, host, opt)...)
	results = append(results,
		remoteCheck(ctx, r, user, host, "listening ports", "ss -H -tuln"),
		remoteCheck(ctx, r, user, host, "egress positive", "getent hosts ubuntu.com >/dev/null && curl -fsI https://ubuntu.com >/dev/null && curl -fsI https://1.1.1.1 >/dev/null"),
	)
	if cfg.Network.Egress.Mode == "restricted" {
		results = append(results, warn("remote", "egress precision", "restricted mode is port/protocol best-effort; global 80/443 and Cloudflare edge 7844 remain open for updates/connectors"))
	}
	if cfg.Network.Ingress != "none" {
		results = append(results, remoteCheck(ctx, r, user, host, "cloudflared", "systemctl is-active cloudflared"))
	}
	return results
}
