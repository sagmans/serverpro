package lifecycle

import (
	"context"
	"errors"
	"fmt"
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
	// tunnelMaxPort is the largest TCP port. A tunnel origin port above it
	// names no port the preflight can place, so it counts as SSH.
	tunnelMaxPort = 65535
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
	// cloudflaredLocalConfigDir is the last directory cloudflared searches for
	// a config when no --config names one; tunnel.CloudflaredConfigDir comes
	// before it.
	cloudflaredLocalConfigDir = "/usr/local/etc/cloudflared"
	// cloudflaredProcessName is the process name of every cloudflared, in a
	// unit, a container, or a shell, since all of them share the host's
	// process table.
	cloudflaredProcessName = "cloudflared"
	// tunnelSSHScheme and tunnelURLSeparator form the ssh:// origin that the
	// host scan's grep looks for in a locally managed cloudflared's settings,
	// next to hostScanProgram's allowlist, in case a form the program does not
	// read hides one.
	tunnelSSHScheme    = "ssh"
	tunnelURLSeparator = "://"
	// tunnelBastionService and tunnelSOCKSProxyService are cloudflared's
	// built-in services that let a client choose the destination (upstream
	// ingress.ServiceBastion and ingress.ServiceSocksProxy). They are named
	// as they are in a finding.
	tunnelBastionService    = "bastion"
	tunnelSOCKSProxyService = "socks-proxy"
	// tunnelHelloWorldService, tunnelHelloWorldFlag, and
	// tunnelStatusServicePrefix are built-in services that answer from
	// cloudflared itself and reach nothing on the host, so they are the only
	// services without a target that pass (upstream ingress.HelloWorldService,
	// ingress.HelloWorldFlag, and the http_status: prefix).
	tunnelHelloWorldService   = "hello_world"
	tunnelHelloWorldFlag      = "hello-world"
	tunnelStatusServicePrefix = "http_status:"
	// tunnelUnixPrefix and tunnelUnixTLSPrefix mark a unix socket origin.
	// cloudflared matches them as prefixes before it parses any URL, so the
	// preflight does too; sshd does not listen on a unix socket.
	tunnelUnixPrefix    = "unix:"
	tunnelUnixTLSPrefix = "unix+tls:"
	// tunnelTCPScheme is the raw TCP origin scheme. It needs an explicit
	// port, because cloudflared adds none for it and the preflight cannot
	// tell which one it reaches.
	tunnelTCPScheme = "tcp"
	// tunnelNetworkRoute and tunnelHostnameRoute name a private IP range or
	// private hostname route in a finding: WARP clients reach every port
	// through one, sshd's included. Neither prints the range or hostname.
	tunnelNetworkRoute  = "a private network route"
	tunnelHostnameRoute = "a private hostname route"
	// tunnelBastionModeRoute and tunnelProxyTypeRoute name the origin
	// settings that let a client pick its destination. Any proxy type counts,
	// not only socks, because the preflight cannot tell what another reaches;
	// its value is not printed, because Cloudflare does not limit it.
	tunnelBastionModeRoute = "bastion mode"
	tunnelProxyTypeRoute   = "an origin proxy type"
	// tunnelEmptyServiceRoute and tunnelUnclassifiedRoute name rules the
	// allowlist cannot place. An unclassified service is not printed, because
	// a form url.Redacted does not understand may hold a credential.
	tunnelEmptyServiceRoute = "a rule without a service"
	tunnelUnclassifiedRoute = "a service serverpro cannot classify"
)

// tunnelWebPorts are the http and https origin schemes with the port each
// one uses when the URL names none.
var tunnelWebPorts = map[string]string{"http": "80", "https": "443"}

// cloudflaredHomeConfigDirs and cloudflaredConfigFiles are where cloudflared
// looks for a config when no --config names one (upstream
// config.DefaultConfigSearchDirectories and DefaultConfigFiles): these
// directories under the running user's home, then the system ones.
var (
	cloudflaredHomeConfigDirs = []string{".cloudflared", ".cloudflare-warp", "cloudflare-warp"}
	cloudflaredConfigFiles    = []string{"config.yml", "config.yaml"}
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

// TunnelSSHRoutes names the rules that may reach sshd, plus any private
// network or hostname route. Each name is the origin service with any URL
// password masked, or a fixed label, so a finding carries no credential and
// never the route's public hostname.
func TunnelSSHRoutes(remote ingress.TunnelRemoteConfig) []string {
	var routes []string
	if remote.PrivateNetworkRoute {
		routes = append(routes, tunnelNetworkRoute)
	}
	if remote.PrivateHostnameRoute {
		routes = append(routes, tunnelHostnameRoute)
	}
	for _, rule := range remote.Rules {
		if name, ok := tunnelRouteToSSH(rule); ok {
			routes = append(routes, name)
		}
	}
	return routes
}

// tunnelRouteToSSH reports whether one ingress rule may reach sshd, and the
// name to report it by. It is an allowlist, because a denylist of SSH forms
// misses the ones nobody listed (cloudflared reads ssh, rdp, smb, and any
// other scheme as a raw TCP origin, and adds port 22 to an ssh one). A rule
// passes only as a built-in that answers from cloudflared itself, a unix
// socket, or an http, https, or tcp origin on a port other than OpenSSHPort,
// and only without an origin setting that lets a client choose the
// destination; the service cases follow cloudflared's own order (upstream
// ingress.validateIngress). Everything else counts, including a service the
// preflight cannot classify: Cloudflare accepted it, so cloudflared reads it
// somehow.
func tunnelRouteToSSH(rule ingress.TunnelIngressRule) (string, bool) {
	switch {
	case rule.BastionMode:
		return tunnelBastionModeRoute, true
	case rule.ProxyType != "":
		return tunnelProxyTypeRoute, true
	case strings.HasPrefix(rule.Service, tunnelUnixPrefix), strings.HasPrefix(rule.Service, tunnelUnixTLSPrefix):
		return "", false
	case strings.HasPrefix(rule.Service, tunnelStatusServicePrefix):
		return "", false
	case rule.Service == tunnelHelloWorldService, rule.Service == tunnelHelloWorldFlag:
		return "", false
	case rule.Service == tunnelBastionService, rule.Service == tunnelSOCKSProxyService:
		return rule.Service, true
	case rule.Service == "":
		return tunnelEmptyServiceRoute, true
	}
	parsed, err := url.Parse(rule.Service)
	if err != nil || parsed.Scheme == "" || parsed.Opaque != "" {
		return tunnelUnclassifiedRoute, true
	}
	name := parsed.Redacted()
	// A port counts as a number, because url.Port keeps it as written and
	// cloudflared dials 0022 as port 22. One that does not parse, or that no
	// TCP port can be, counts as SSH, since the preflight cannot tell where
	// it leads.
	reachesSSH := func(port string) bool {
		n, err := strconv.Atoi(port)
		return err != nil || n > tunnelMaxPort || n == OpenSSHPort
	}
	if parsed.Scheme == tunnelTCPScheme {
		return name, parsed.Hostname() == "" || parsed.Port() == "" || reachesSSH(parsed.Port())
	}
	defaultPort, web := tunnelWebPorts[parsed.Scheme]
	if !web || parsed.Hostname() == "" {
		return name, true
	}
	port := parsed.Port()
	if port == "" {
		port = defaultPort
	}
	return name, reachesSSH(port)
}

// OpenSSHDisableScript turns OpenSSH off on a host that Tailscale SSH already
// serves. Each preflight condition is a way the change could cut off the
// operator or a workload, so the script stops before it changes anything when
// one holds: tailscaled down, Tailscale SSH off, an established OpenSSH
// session, an OpenSSH login in the recent window (only the method and the
// user are printed, never a source address), or a cloudflared ingress that
// may reach SSH or cannot be read. The scan judges every origin it finds
// with the remote allowlist (hostScanProgram) and also greps for ssh:// and
// port 22 with any leading zeros. It covers a --url or TUNNEL_URL origin and
// the --bastion and --socks5 modes in a cloudflared unit, its environment
// files, or the defaults file; each config a unit names with --config; and
// every directory cloudflared searches for a config without one, under every
// home in the account database and every HOME a unit sets, and on the
// system. A cloudflared process outside the cloudflared units (a container,
// or one run by hand), found by name or by the executable it runs, also
// stops the script, because its settings are not on the host to read.
// Ingress kept at Cloudflare is CheckTunnelSSHIngress's part, run before
// this script.
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
	// The grep backs up the allowlist: cloudflared reads a port as a number,
	// so 22 counts with any leading zeros.
	sshPort := ":0*" + port + "([^0-9]|$)"
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
cloudflared_defaults=` + shell.Quote(cloudflaredDefaultsPath) + `
# The lists hold one entry per line, so the loops split them on newlines
# alone and a home path with a space stays one entry.
system_config_dirs=` + shell.Quote(tunnel.CloudflaredConfigDir+"\n"+cloudflaredLocalConfigDir) + `
home_config_dirs=` + shell.Quote(strings.Join(cloudflaredHomeConfigDirs, "\n")) + `
config_files=` + shell.Quote(strings.Join(cloudflaredConfigFiles, "\n")) + `
newline='
'
host_scan=` + shell.Quote(hostScanProgram()) + `
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
# scan_config stops when the cloudflared config at $1, a file or a
# directory, may reach SSH or cannot be read. A path that does not exist is
# skipped: cloudflared skips it too. find, like grep -r, follows a symlink
# only when it is $1 itself; the config file names are scanned outright.
scan_config() {
  [ -e "$1" ] || return 0
  rc=0
  grep -rEiqs '` + tunnelSSHScheme + tunnelURLSeparator + `|` + sshPort + `' "$1" || rc=$?
  case $rc in
    0) stop "a cloudflared ingress in $1 routes to SSH" ;;
    1) ;;
    *) stop "$1 could not be read" ;;
  esac
  reason=$(find -H "$1" -type f -exec awk -v mode=` + hostScanConfigMode + ` "$host_scan" {} +) || stop "$1 could not be read"
  [ -z "$reason" ] || stop "a cloudflared config in $1 may reach SSH (${reason%%"$newline"*}); remove or retarget it, then rerun"
}
# A legacy or quick tunnel takes its origin, or bastion or SOCKS5 mode, from
# a flag or its variable, in the unit or in an environment file it reads.
# These settings also carry the tunnel token, so the checks only print a
# label or a path, never a value. The grep's port match is tied to the flag,
# because the unit's ExecStart also shows start times such as 12:22:05.
unit_settings=$(systemctl show -p ExecStart -p Environment --value ` + shell.Quote(tunnel.CloudflaredUnit) + ` ` + shell.Quote(cloudflaredUnitPattern) + `) || stop "systemctl could not read the cloudflared units"
env_files=$(systemctl show -p EnvironmentFiles --value ` + shell.Quote(tunnel.CloudflaredUnit) + ` ` + shell.Quote(cloudflaredUnitPattern) + `) || stop "systemctl could not read the cloudflared environment files"
env_files=$(printf '%s\n%s\n' "$cloudflared_defaults" "$env_files" | sed 's/ (ignore_errors=[a-z]*)$//') || stop "the cloudflared environment files could not be listed"
settings=$unit_settings
set -f
old_ifs=$IFS
IFS=$newline
for env_file in $env_files; do
  [ -e "$env_file" ] || continue
  content=$(cat "$env_file") || stop "$env_file could not be read"
  settings=$settings$newline$content
done
IFS=$old_ifs
set +f
rc=0
printf '%s\n' "$settings" | grep -Eiq '` + tunnelSSHScheme + tunnelURLSeparator + `|(--` + cloudflaredURLFlag + `[= ]+|` + cloudflaredURLEnv + `=)["'"'"']?[^ ;"'"'"']*` + sshPort + `' || rc=$?
case $rc in
  0) stop "a cloudflared unit or one of its environment files routes to SSH" ;;
  1) ;;
  *) stop "the cloudflared unit settings could not be searched" ;;
esac
reason=$(printf '%s\n' "$settings" | awk -v mode=` + hostScanSettingsMode + ` "$host_scan") || stop "the cloudflared unit settings could not be searched"
[ -z "$reason" ] || stop "a cloudflared unit or one of its environment files may reach SSH ($reason); remove or retarget it, then rerun"
# A --config path, in the unit or in an environment variable it expands,
# replaces the default search. Only an absolute path can be read the way
# cloudflared reads it, and a named config that is missing cannot be proven
# safe, so both stop the script.
rc=0
config_args=$(printf '%s\n' "$settings" | grep -oE -- '(^|[^A-Za-z0-9_-])--?config[= ]+["'"'"']?[^ ;"'"'"']+') || rc=$?
case $rc in
  0|1) ;;
  *) stop "the cloudflared unit settings could not be searched for --config" ;;
esac
configs=$(printf '%s\n' "$config_args" | sed -E 's/^[^-]*--?config[= ]+["'"'"']?//') || stop "the cloudflared --config paths could not be read"
# cloudflared expands ~ with HOME, which a unit can set apart from the
# account database, so those homes are searched too.
unit_homes=$(printf '%s\n' "$settings" | awk -v mode=` + hostScanHomeMode + ` "$host_scan") || stop "the cloudflared unit settings could not be searched for HOME"
settings=
passwd_db=$(getent passwd) || stop "the account database could not be read"
passwd_homes=$(printf '%s\n' "$passwd_db" | cut -d: -f6) || stop "the home directories could not be listed"
homes=$(printf '%s\n%s\n' "$passwd_homes" "$unit_homes" | sort -u) || stop "the home directories could not be listed"
set -f
old_ifs=$IFS
IFS=$newline
for home in $unit_homes; do
  case $home in
    (/*) ;;
    (*) stop "a cloudflared unit sets HOME to a path that is not absolute, so its config search cannot be read here" ;;
  esac
done
for config in $configs; do
  case $config in
    (/*) ;;
    (*) stop "a cloudflared unit names config $config, which is not an absolute path" ;;
  esac
  [ -e "$config" ] || stop "cloudflared config $config, named by a cloudflared unit, does not exist"
  scan_config "$config"
done
search_dirs=
for home in $homes; do
  [ -n "$home" ] || continue
  for dir in $home_config_dirs; do
    search_dirs=$search_dirs$newline$home/$dir
  done
done
for dir in $system_config_dirs; do
  search_dirs=$search_dirs$newline$dir
done
# grep -r does not follow a symlink inside a directory, while cloudflared
# reads a symlinked config file, so the config file names are scanned
# outright as well.
for dir in $search_dirs; do
  [ -n "$dir" ] || continue
  scan_config "$dir"
  for file in $config_files; do
    scan_config "$dir/$file"
  done
done
IFS=$old_ifs
set +f
rc=0
pids=$(pgrep -x ` + cloudflaredProcessName + `) || rc=$?
case $rc in
  0|1) ;;
  *) stop "pgrep could not list the cloudflared processes" ;;
esac
# pgrep matches the process name, which a launcher run through a symlink or
# under another name changes, so the executable each process runs counts
# too. A process without one (a kernel thread) or gone by now is skipped.
for proc in /proc/[0-9]*; do
  exe=$(readlink "$proc/exe" 2>/dev/null) || continue
  case ${exe##*/} in
    (` + cloudflaredProcessName + `*) pids="$pids ${proc##*/}" ;;
  esac
done
pids=$(printf '%s\n' $pids | sort -nu)
for pid in $pids; do
  if ! cgroup=$(cat "/proc/$pid/cgroup" 2>/dev/null); then
    [ -d "/proc/$pid" ] || continue
    stop "the cgroup of cloudflared process $pid could not be read"
  fi
  unit=$(printf '%s\n' "$cgroup" | awk -F: '$1 == "0" || $2 == "name=systemd" { n = split($3, part, "/"); print part[n]; exit }') || stop "the cgroup of cloudflared process $pid could not be parsed"
  case $unit in
    (` + cloudflaredUnitPattern + `) ;;
    (*) stop "cloudflared process $pid runs outside the cloudflared units (in ${unit:-an unknown cgroup}), for example in a container; its ingress cannot be read here: make sure it does not route to SSH, stop it, and rerun" ;;
  esac
done

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
