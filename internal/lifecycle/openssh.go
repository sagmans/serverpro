package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/hostplatform"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/state"
	"github.com/sagmans/serverpro/internal/tunnel"
)

const (
	// OpenSSHStatePath records each ssh unit's enablement and the condition
	// file as they were before serverpro turned OpenSSH off, so an operator
	// who opts back in restores the host's own state, not a guess.
	OpenSSHStatePath = "/var/lib/serverpro/openssh-disabled.state"
	// OpenSSHConditionPath is the flag both Ubuntu ssh units test with
	// ConditionPathExists=!. It keeps sshd from starting when a unit is
	// unmasked by hand or a package upgrade tries to start it.
	OpenSSHConditionPath = "/etc/ssh/sshd_not_to_be_run"
	// OpenSSHWarningPrefix starts each recovery warning. It equals the doctor
	// drift warning prefix, so doctor --fix reports the warnings after a
	// repair succeeds.
	OpenSSHWarningPrefix = "warn: "
	// openSSHStoppedPrefix starts every reason the script stops, so operators
	// and tests can find it in output.
	openSSHStoppedPrefix = "openssh disable stopped: "
	// openSSHLoginWindowDays is how far back an OpenSSH login stops the
	// script: a login in that window means someone may still depend on sshd.
	openSSHLoginWindowDays = 30
	// OpenSSHPort is the port a stray sshd or socket listener would hold.
	OpenSSHPort = 22
	// tailscaledUnit is the daemon that serves Tailscale SSH, the only admin
	// path left once OpenSSH is off.
	tailscaledUnit = "tailscaled"
	// passwordStatusUsable is the passwd -S status of an account with a
	// usable password, which a provider console login needs.
	passwordStatusUsable = "P"
	// cloudflaredDefaultsPath is the options file the distribution's
	// cloudflared units read. A legacy or quick tunnel can take its origin
	// from it instead of from an ingress file.
	cloudflaredDefaultsPath = "/etc/default/cloudflared"
	// cloudflaredUnitPattern covers the running units someone added next to
	// tunnel.CloudflaredUnit for another tunnel. systemctl matches a pattern
	// only against loaded units, so the managed unit is also named outright.
	cloudflaredUnitPattern = "cloudflared*.service"
	// tunnelSSHScheme is the origin scheme of a tunnel route to an SSH server.
	tunnelSSHScheme = "ssh"
	// tunnelURLSeparator splits a service's scheme from its address. A
	// service without it is a host:port, a built-in, or a unix socket.
	tunnelURLSeparator = "://"
	// tunnelBastionService and tunnelSOCKS5Service are built-in services
	// that let a client choose the destination, so they reach sshd too.
	tunnelBastionService = "bastion"
	tunnelSOCKS5Service  = "socks5"
	// tunnelSOCKSProxyType is the originRequest proxy type that turns a rule
	// into a SOCKS proxy, with the same reach as tunnelSOCKS5Service.
	tunnelSOCKSProxyType = "socks"
	// tunnelWARPRoutingRoute names WARP routing in a finding: it carries
	// private network traffic to any port a route covers, sshd's included.
	tunnelWARPRoutingRoute = "WARP routing"
)

var (
	// ErrTunnelIngressUnread means the tunnel's remote ingress could not be
	// read, so the preflight cannot tell whether a route needs sshd.
	ErrTunnelIngressUnread = errors.New("the Cloudflare tunnel ingress could not be read")
	// ErrTunnelRoutesSSH means a remote ingress rule reaches sshd, which
	// masking the ssh units would cut off.
	ErrTunnelRoutesSSH = errors.New("a Cloudflare tunnel route reaches SSH")
)

// TunnelIngressReader reads the ingress rules of the server's remotely
// managed Cloudflare tunnel. Serverpro creates tunnels with config_src
// cloudflare, so their routes live at Cloudflare and the host-side preflight
// in OpenSSHDisableScript cannot see them.
type TunnelIngressReader interface {
	TunnelIngress(context.Context, string) (ingress.TunnelRemoteConfig, error)
}

// CheckTunnelSSHIngress is the remote half of the OpenSSH preflight: it runs
// before OpenSSHDisableScript, in create and in doctor --fix, and stops the
// change when a dashboard route such as ssh://localhost:22 would lose its
// origin. It fails closed: with a tunnel in state, no reader or a failed read
// stops the change as surely as an SSH route does. A server without a tunnel
// has no remote ingress to read.
func CheckTunnelSSHIngress(ctx context.Context, reader TunnelIngressReader, tunnelID string) error {
	if tunnelID == "" {
		return nil
	}
	if reader == nil {
		return fmt.Errorf("%s%w: no Cloudflare client", openSSHStoppedPrefix, ErrTunnelIngressUnread)
	}
	remote, err := reader.TunnelIngress(ctx, tunnelID)
	if err != nil {
		return fmt.Errorf("%s%w: %w", openSSHStoppedPrefix, ErrTunnelIngressUnread, err)
	}
	if routes := TunnelSSHRoutes(remote); len(routes) > 0 {
		return fmt.Errorf("%s%w: %s; remove or retarget them, then rerun", openSSHStoppedPrefix, ErrTunnelRoutesSSH, strings.Join(routes, ", "))
	}
	return nil
}

// TunnelSSHRoutes names the rules that can reach sshd: an ssh:// origin, any
// origin on the OpenSSH port, and the rules that let a client pick its own
// destination (bastion mode, a SOCKS proxy), plus WARP routing when it is on.
// Each name is the origin service with any URL password masked, so a finding
// carries no credential and never the route's public hostname.
func TunnelSSHRoutes(remote ingress.TunnelRemoteConfig) []string {
	var routes []string
	if remote.WARPRouting {
		routes = append(routes, tunnelWARPRoutingRoute)
	}
	for _, rule := range remote.Rules {
		switch {
		case rule.BastionMode:
			routes = append(routes, "bastion mode")
		case strings.EqualFold(rule.ProxyType, tunnelSOCKSProxyType):
			routes = append(routes, "SOCKS proxy")
		case rule.Service == tunnelBastionService, rule.Service == tunnelSOCKS5Service:
			routes = append(routes, rule.Service)
		default:
			if name, ok := tunnelServiceToSSH(rule.Service); ok {
				routes = append(routes, name)
			}
		}
	}
	return routes
}

// tunnelServiceToSSH reports whether a service URL or host:port reaches sshd,
// and the name to report it by. A URL that does not parse counts: Cloudflare
// accepted it, so cloudflared reads it somehow, and the preflight cannot rule
// out that it reaches sshd.
func tunnelServiceToSSH(service string) (string, bool) {
	port := strconv.Itoa(OpenSSHPort)
	if !strings.Contains(service, tunnelURLSeparator) {
		_, servicePort, err := net.SplitHostPort(service)
		return service, err == nil && servicePort == port
	}
	parsed, err := url.Parse(service)
	if err != nil {
		return "a service URL that does not parse", true
	}
	return parsed.Redacted(), strings.EqualFold(parsed.Scheme, tunnelSSHScheme) || parsed.Port() == port
}

// OpenSSHDisableScript turns OpenSSH off on a host that Tailscale SSH already
// serves. Each preflight condition is a way the change could cut off the
// operator or a workload, so the script stops before it changes anything when
// one holds: tailscaled down, Tailscale SSH off, an established OpenSSH
// session, an OpenSSH login in the recent window (only the method and the
// user are printed, never a source address), or a cloudflared ingress that
// routes to SSH: an ingress file under /etc/cloudflared, or a --url or
// TUNNEL_URL origin in a cloudflared unit or its defaults file. Ingress kept
// at Cloudflare is CheckTunnelSSHIngress's part, run before this script.
// An admin user without a password and an active provider console
// agent (consoleAgents, declared by the compute adapter) are only warnings:
// they change how to recover the host, not whether the change is safe.
//
// The script records the original unit states once, then disables, stops,
// and masks both units and creates the condition file. Masking stops a
// socket or a dependency from starting sshd again. The openssh packages stay
// installed for rollback and package updates. A rerun is a no-op that keeps
// the first record.
func OpenSSHDisableScript(admin string, consoleAgents []string) string {
	socket := shell.Quote(hostplatform.OpenSSHSocketUnit)
	service := shell.Quote(hostplatform.OpenSSHServiceUnit)
	port := strconv.Itoa(OpenSSHPort)
	agents := make([]string, len(consoleAgents))
	for i, agent := range consoleAgents {
		agents[i] = shell.Quote(agent)
	}
	return `set -eu
export LC_ALL=C
umask 077
units="` + hostplatform.OpenSSHSocketUnit + ` ` + hostplatform.OpenSSHServiceUnit + `"
state_file=` + shell.Quote(OpenSSHStatePath) + `
state_dir=` + shell.Quote(path.Dir(OpenSSHStatePath)) + `
condition=` + shell.Quote(OpenSSHConditionPath) + `
cloudflared_dir=` + shell.Quote(tunnel.CloudflaredConfigDir) + `
cloudflared_defaults=` + shell.Quote(cloudflaredDefaultsPath) + `
stop() {
  printf '` + openSSHStoppedPrefix + `%s\n' "$1" >&2
  exit 1
}
warn() {
  printf '` + OpenSSHWarningPrefix + `%s\n' "$1"
}

systemctl is-active --quiet ` + tailscaledUnit + ` || stop "tailscaled is not active"
prefs=$(tailscale debug prefs 2>/dev/null) || stop "tailscale debug prefs failed"
printf '%s\n' "$prefs" | grep -Eq '"RunSSH":[[:space:]]*true' || stop "Tailscale SSH is off (RunSSH is not true)"
sessions=$(ss -Htn state established '( sport = :` + port + ` )') || stop "ss could not list established sessions"
[ -z "$sessions" ] || stop "an OpenSSH session is established on port ` + port + `; end it and rerun"
journal=$(journalctl -q -o cat --no-pager --since '` + strconv.Itoa(openSSHLoginWindowDays) + ` days ago' _COMM=sshd _COMM=sshd-session) || stop "journalctl could not read sshd logs"
authlog=$(find /var/log -maxdepth 1 -type f -name 'auth.log*' -mtime -` + strconv.Itoa(openSSHLoginWindowDays) + ` -exec zcat -f {} +) || stop "auth.log could not be read"
cutoff=$(date -u -d '` + strconv.Itoa(openSSHLoginWindowDays) + ` days ago' +%Y-%m-%dT%H:%M:%S)
# An auth.log line with an ISO timestamp counts only inside the window. A
# line in the classic syslog format has no year, so it always counts: that
# can stop the script for an old login, never miss a recent one.
logins=$({
  printf '%s\n' "$journal"
  printf '%s\n' "$authlog" | awk -v cutoff="$cutoff" '/ sshd(-session)?\[[0-9]+\]: Accepted / { if ($1 ~ /^[0-9][0-9][0-9][0-9]-/ && substr($1, 1, 19) < cutoff) next; print }'
} | awk '{ for (i = 1; i < NF; i++) if ($i == "Accepted" && $(i + 2) == "for") { print $(i + 1) " for " $(i + 3); break } }' | sort -u | awk 'NR > 1 { printf ", " } { printf "%s", $0 }')
[ -z "$logins" ] || stop "OpenSSH logins in the last ` + strconv.Itoa(openSSHLoginWindowDays) + ` days: $logins"
if [ -d "$cloudflared_dir" ]; then
  rc=0
  grep -rEqs 'ssh://|:` + port + `([^0-9]|$)' "$cloudflared_dir" || rc=$?
  case $rc in
    0) stop "a cloudflared ingress under $cloudflared_dir routes to SSH" ;;
    1) ;;
    *) stop "$cloudflared_dir could not be read" ;;
  esac
fi
# A legacy or quick tunnel takes its origin from --url or TUNNEL_URL. These
# settings also carry the tunnel token, so grep only answers yes or no and
# nothing is printed. The port match is tied to the flag, because the unit's
# ExecStart also shows start times such as 12:22:05.
unit_settings=$(systemctl show -p ExecStart -p Environment --value ` + shell.Quote(tunnel.CloudflaredUnit) + ` ` + shell.Quote(cloudflaredUnitPattern) + `) || stop "systemctl could not read the cloudflared units"
defaults=
if [ -e "$cloudflared_defaults" ]; then
  defaults=$(cat "$cloudflared_defaults") || stop "$cloudflared_defaults could not be read"
fi
rc=0
printf '%s\n%s\n' "$unit_settings" "$defaults" | grep -Eq '` + tunnelSSHScheme + tunnelURLSeparator + `|(--url[= ]+|TUNNEL_URL=)["'"'"']?[^ ;"'"'"']*:` + port + `([^0-9]|$)' || rc=$?
case $rc in
  0) stop "a cloudflared unit or $cloudflared_defaults routes to SSH" ;;
  1) ;;
  *) stop "the cloudflared unit settings could not be searched" ;;
esac

admin=` + shell.Quote(admin) + `
password_status=$(passwd -S "$admin" 2>/dev/null | awk '{ print $2 }') || true
[ "$password_status" = ` + passwordStatusUsable + ` ] || warn "admin user $admin has no usable password: a provider console cannot log in, so recovery needs rescue mode"
for agent in ` + strings.Join(agents, " ") + `; do
  if systemctl is-active --quiet "$agent"; then
    warn "$agent is active: the provider console it serves logs in through sshd and stops working; use the provider's recovery console"
  fi
done

if [ ! -e "$state_file" ]; then
  [ -d "$state_dir" ] || install -d -m 0700 "$state_dir"
  tmp=$(mktemp "$state_dir/.openssh-disabled.XXXXXX")
  trap 'rm -f "$tmp"' EXIT
  for unit in $units; do
    unit_state=$(systemctl is-enabled "$unit" 2>/dev/null) || true
    printf '%s=%s\n' "$unit" "${unit_state:-unknown}" >>"$tmp"
  done
  if [ -e "$condition" ]; then condition_state=present; else condition_state=absent; fi
  printf 'condition=%s\n' "$condition_state" >>"$tmp"
  chmod 0600 "$tmp"
  mv -f "$tmp" "$state_file"
  trap - EXIT
fi

systemctl disable --now ` + socket + ` ` + service + `
systemctl mask ` + socket + ` ` + service + `
touch "$condition"

for unit in $units; do
  if systemctl is-active --quiet "$unit"; then stop "$unit is still active"; fi
done
remaining=$(` + OpenSSHListenersCommand() + `) || stop "ss could not list TCP listeners"
remaining=$(printf '%s' "$remaining" | tr '\n' ' ')
[ -z "$remaining" ] || stop "a TCP listener on port ` + port + ` or an sshd listener remains: ${remaining% }"
printf 'OpenSSH off: %s masked; state in %s\n' "$units" "$state_file"
`
}

// OpenSSHListenersCommand prints, one per line, the name of each process that
// holds a TCP listener on the OpenSSH port, and of any sshd that listens on
// another port. It exits non-zero when ss cannot list, so a failed read never
// looks like a clean host. The disable script verifies with it and doctor
// reads with it, so both agree on what "OpenSSH still listens" means.
func OpenSSHListenersCommand() string {
	return `listeners=$(ss -Hltnp) && printf '%s\n' "$listeners" | awk -v port=":` + strconv.Itoa(OpenSSHPort) + `" '
  $4 ~ port "$" || /"sshd"/ {
    name = $0
    if (sub(/.*users:\(\("/, "", name)) { sub(/".*/, "", name) } else { name = "unknown" }
    print name
  }' | sort -u`
}

// disableOpenSSH runs last in create, after the bootstrap reached the host
// through Tailscale SSH, so Tailscale SSH is proven before sshd goes away.
// With OpenSSH enabled in config it does nothing. Without a remote host it
// fails closed, as the egress lockdown does: the config promises OpenSSH off,
// and a create that never applied it must not report success. The tunnel's
// remote ingress is read first, because the host script cannot see it.
func disableOpenSSH(ctx context.Context, r remote.Runner, creator ComputeCreator, tunnels TunnelIngressReader, cfg config.Config, st state.State) error {
	if cfg.Access.OpenSSH == config.OpenSSHEnabled {
		return nil
	}
	if r == nil || st.Tailscale.Name == "" {
		return fmt.Errorf("openssh disable: no remote host to turn OpenSSH off on")
	}
	if err := CheckTunnelSSHIngress(ctx, tunnels, st.Cloudflare.TunnelID); err != nil {
		// The error already starts with the script's own stop prefix.
		return err
	}
	if _, err := r.Run(ctx, cfg.Admin.Username, st.Tailscale.Name, OpenSSHDisableScript(cfg.Admin.Username, sshConsoleAgents(creator))); err != nil {
		return fmt.Errorf("openssh disable: %w", err)
	}
	return nil
}

// sshConsoleAgents reads the agents from the creator because create holds the
// compute adapter, not the resolved provider doctor gets.
func sshConsoleAgents(creator ComputeCreator) []string {
	declared, ok := creator.(compute.SSHConsoleAgentsProvider)
	if !ok {
		return nil
	}
	return declared.SSHConsoleAgents()
}
