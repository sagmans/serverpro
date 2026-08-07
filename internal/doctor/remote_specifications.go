package doctor

import (
	"context"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/tailscaletools"
)

// remoteCheckSpecification keeps batch authority inseparable from the check
// that consumes it, preventing new diagnostics from bypassing either path.
type remoteCheckSpecification struct {
	readCommands []string
	liveCommands []string
	run          func(context.Context, remote.Runner, string, string, Options) []Result
}

func remoteCheckSpecifications(cfg config.Config) []remoteCheckSpecification {
	user := cfg.Admin.Username
	specifications := []remoteCheckSpecification{
		remoteSudoSpecification(user),
		remoteCloudInitSpecification(cloudInitStatusLogPath(cfg)),
		remoteFixableSpecification("admin user", "id "+shell.Quote(user), ""),
		remoteFixableSpecification("ufw active", "ufw status verbose | grep -Fx 'Status: active'", "ufw --force enable"),
		remoteFixableSpecification("ufw default deny incoming", "ufw status verbose | grep -F 'Default: deny (incoming)'", "ufw default deny incoming && ufw --force reload"),
		remoteFixableSpecification("ufw ssh ingress", ufwSSHIngressCommand(), "ufw delete allow OpenSSH || true\nufw delete allow 22/tcp || true\nufw delete allow 22 || true\nufw --force reload"),
	}
	for _, setting := range []struct {
		name    string
		keyword string
	}{
		{name: "sshd root login", keyword: sshdKeywordPermitRootLogin},
		{name: "sshd password auth", keyword: sshdKeywordPasswordAuthentication},
		{name: "sshd keyboard-interactive auth", keyword: sshdKeywordKbdInteractiveAuthentication},
	} {
		specifications = append(specifications, remoteSSHDSettingSpecification(setting.name, setting.keyword))
	}
	specifications = append(specifications, remoteFixableSpecification("sshd challenge-response auth", sshdChallengeResponseCommand(), sshdSettingsFixCommand()))
	for _, setting := range []struct {
		name    string
		keyword string
	}{
		{name: "sshd x11 forwarding", keyword: sshdKeywordX11Forwarding},
		{name: "sshd agent forwarding", keyword: sshdKeywordAllowAgentForwarding},
		{name: "sshd tcp forwarding", keyword: sshdKeywordAllowTCPForwarding},
		{name: "sshd tunnel forwarding", keyword: sshdKeywordPermitTunnel},
		{name: "sshd open forwarding", keyword: sshdKeywordPermitOpen},
	} {
		specifications = append(specifications, remoteSSHDSettingSpecification(setting.name, setting.keyword))
	}
	specifications = append(specifications,
		remoteFixableSpecification("apparmor", "systemctl is-active apparmor && aa-status --enabled", "systemctl enable --now apparmor"),
		remoteFixableSpecification("unattended upgrades", "systemctl is-enabled unattended-upgrades || systemctl is-enabled apt-daily.timer", "systemctl enable --now unattended-upgrades || systemctl enable --now apt-daily.timer"),
		remoteFixableSpecification("journald persistent", "test -d /var/log/journal", "mkdir -p /var/log/journal && systemctl restart systemd-journald"),
		remoteToolSpecification(user),
		remoteFixableSpecification("listening ports", "ss -H -tuln", ""),
		remoteDNSResolutionSpecification(),
		remoteFixableSpecification("egress positive", "getent hosts ubuntu.com >/dev/null && curl -fsI https://ubuntu.com >/dev/null && curl -fsI https://1.1.1.1 >/dev/null", ""),
	)
	if cfg.Network.Egress.Mode == "restricted" {
		specifications = append(specifications,
			remoteFixableSpecification("ufw ssh egress", "ufw status numbered verbose | grep -E '(^|[[:space:]])22/tcp[[:space:]].*ALLOW OUT'", "ufw allow out 22/tcp && ufw --force reload"),
			remoteStaticSpecification(warn("remote", "egress precision", "restricted mode is port/protocol best-effort; global 22/80/443 and Cloudflare edge 7844 remain open for git/updates/connectors")),
		)
	}
	if cfg.Git.Access == config.GitAccessAccountKey {
		specifications = append(specifications, remoteGitIdentitySpecifications(cfg)...)
	}
	if cfg.Network.Ingress != "none" {
		specifications = append(specifications, remoteFixableSpecification("cloudflared", "systemctl is-active cloudflared", ""))
	}
	return specifications
}

func remoteFixableSpecification(name, readCommand, liveCommand string) remoteCheckSpecification {
	specification := remoteCheckSpecification{readCommands: []string{readCommand}}
	if liveCommand != "" {
		specification.liveCommands = []string{liveCommand}
	}
	specification.run = func(ctx context.Context, runner remote.Runner, user, host string, options Options) []Result {
		return []Result{remoteFixableCheck(ctx, runner, user, host, name, readCommand, liveCommand, options)}
	}
	return specification
}

func remoteSSHDSettingSpecification(name, keyword string) remoteCheckSpecification {
	value := sshdHardeningExpectations[keyword]
	return remoteFixableSpecification(name, sshdSettingValueCommand(keyword, value), sshdSettingsFixCommand())
}

func remoteSudoSpecification(user string) remoteCheckSpecification {
	return remoteCheckSpecification{
		readCommands: []string{sudoPasswordRequiredCommand(user)},
		liveCommands: []string{sudoPasswordFixCommand(user)},
		run: func(ctx context.Context, runner remote.Runner, user, host string, options Options) []Result {
			return []Result{remoteSudoPasswordRequiredCheck(ctx, runner, user, host, options)}
		},
	}
}

func remoteCloudInitSpecification(logPath string) remoteCheckSpecification {
	return remoteCheckSpecification{
		readCommands: []string{cloudInitWaitCommand, cloudInitLongCommand},
		run: func(ctx context.Context, runner remote.Runner, user, host string, _ Options) []Result {
			return []Result{remoteCloudInitCheck(ctx, runner, user, host, logPath)}
		},
	}
}

func remoteToolSpecification(user string) remoteCheckSpecification {
	checks := remoteToolDefinitions(user)
	readCommands := make([]string, len(checks))
	for i, check := range checks {
		readCommands[i] = check.Command
	}
	return remoteCheckSpecification{
		readCommands: readCommands,
		liveCommands: []string{bootstraptools.InstallScriptForUser(user), bootstraptools.ManagedPackageRefreshCommand(), tailscaletools.UpdateScript()},
		run: func(ctx context.Context, runner remote.Runner, user, host string, options Options) []Result {
			return remoteToolChecks(ctx, runner, user, host, options)
		},
	}
}

// remoteDNSResolutionSpecification isolates DNS failure from egress failure:
// the canary command fails only when name resolution breaks, and remediation
// points at the tailnet/host resolver instead of the network path.
func remoteDNSResolutionSpecification() remoteCheckSpecification {
	command := dnsResolutionCommand()
	return remoteCheckSpecification{
		readCommands: []string{command},
		run: func(ctx context.Context, runner remote.Runner, user, host string, _ Options) []Result {
			out, err := runner.Run(ctx, user, host, command)
			if err != nil {
				return []Result{fail("remote", "dns resolution", err.Error(), dnsResolutionRemediation)}
			}
			return []Result{pass("remote", "dns resolution", trim(out))}
		},
	}
}

// remoteGitIdentitySpecifications guards the account-key development setup:
// config-only drift is fixed remotely, while key/PAT registration stays a
// human step because remediation requires GitHub account access.
func remoteGitIdentitySpecifications(cfg config.Config) []remoteCheckSpecification {
	user := cfg.Admin.Username
	specifications := []remoteCheckSpecification{
		remoteFixableSpecification("git identity", gitIdentityReadCommand(user, cfg.Git.Identity), gitIdentityFixCommand(user, cfg.Git.Identity)),
		remoteFixableSpecification("github ssh auth", githubSSHAuthReadCommand(user), ""),
		remoteFixableSpecification("gh auth", ghAuthReadCommand(user), ""),
	}
	if cfg.Git.Signing {
		specifications = append(specifications, remoteFixableSpecification("git signing", gitSigningReadCommand(user), gitSigningFixCommand(user)))
	}
	return specifications
}

func remoteStaticSpecification(result Result) remoteCheckSpecification {
	return remoteCheckSpecification{run: func(context.Context, remote.Runner, string, string, Options) []Result {
		return []Result{result}
	}}
}
