package network

import (
	"strconv"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/shell"
)

const (
	// EgressBlockedProbeHost and EgressBlockedProbePort name a connect that
	// restricted egress must drop: DNS over TLS on a public resolver is always
	// served, so a connect proves the path is open, and the port is not in
	// restrictedAllowOut. Provisioning and doctor prove the lockdown with the
	// same connect, so they cannot disagree on whether it is in force.
	EgressBlockedProbeHost = "1.1.1.1"
	EgressBlockedProbePort = 853
	// EgressBlockedProbeTimeoutSeconds bounds the connect: a dropped SYN
	// otherwise waits out the kernel's whole retry budget.
	EgressBlockedProbeTimeoutSeconds = 3
	// egressAllowedProbeHost and egressAllowedProbePort name a connect that
	// restricted egress must keep: every serverpro host reaches its operator
	// over Tailscale, so a lockdown that cut the coordination server would
	// strand the host. The name also proves DNS still resolves.
	egressAllowedProbeHost = "controlplane.tailscale.com"
	egressAllowedProbePort = 443
	// egressAllowedProbeAttempts and egressAllowedProbeTimeoutSeconds absorb
	// a slow resolver or one lost SYN right after the reload, so only a path
	// the rules really block fails the lockdown.
	egressAllowedProbeAttempts       = 3
	egressAllowedProbeTimeoutSeconds = 5
	egressAllowedProbeRetrySeconds   = 2
	// LockdownFailedPrefix starts every reason the lockdown gives when it did
	// not take effect, so operators and tests can find it in output.
	LockdownFailedPrefix = "egress lockdown did not take effect: "
)

// restrictedAllowOut is every outbound rule restricted egress keeps. The
// lockdown only adds rules and changes the default, so rules other tools
// added (for example the Docker bridge rules from the bootstrap tools) stay.
var restrictedAllowOut = []string{
	"53",      // DNS for apt, git, and tools before tailscaled takes over.
	"123/udp", // NTP: TLS and Tailscale keys fail on a skewed clock.
	"80/tcp",  // Ubuntu apt mirrors still serve over plain HTTP.
	"443/tcp", // HTTPS, Tailscale DERP, and package repositories.
	// outbound 22: git-over-ssh (pull/push/clone) breaks under HTTPS-only egress
	"22/tcp",
	"7844/tcp",  // Cloudflare tunnel edge (HTTP/2).
	"7844/udp",  // Cloudflare tunnel edge (QUIC).
	"41641/udp", // Tailscale direct connections.
}

// RestrictedAllowOut returns the outbound rules restricted egress keeps, so
// doctor can prove each one is present without its own copy of the list.
func RestrictedAllowOut() []string {
	return append([]string(nil), restrictedAllowOut...)
}

// LockdownScript converges UFW's outbound policy to config. Provisioning,
// server bootstrap, and the doctor repair all run it. In restricted mode it
// then proves the result and exits non-zero otherwise, because a default
// that did not change (UFW inactive, or a later tool that reset it) would
// leave the host open while every caller reported success.
func LockdownScript(cfg config.Config) string {
	if cfg.Network.Egress.Mode == config.EgressModeOpen {
		return "ufw default allow outgoing\nufw --force reload\n"
	}
	var script strings.Builder
	script.WriteString("set -eu\nufw default deny outgoing\n")
	for _, rule := range restrictedAllowOut {
		script.WriteString("ufw allow out " + rule + "\n")
	}
	script.WriteString("ufw --force reload\n")
	script.WriteString(lockdownVerifyScript())
	return script.String()
}

// lockdownVerifyScript fails closed on both halves of the lockdown: the UFW
// default (policy) and a real connect to a port the allow list never opens
// (effect). An inactive UFW prints no default, so it fails too. It also
// proves the allow list still carries a path the host cannot lose, because a
// lockdown that strands the host is as wrong as one that leaves it open.
func lockdownVerifyScript() string {
	target := EgressBlockedProbeHost + ":" + strconv.Itoa(EgressBlockedProbePort)
	connect := "exec 3<>/dev/tcp/" + EgressBlockedProbeHost + "/" + strconv.Itoa(EgressBlockedProbePort)
	allowed := egressAllowedProbeHost + ":" + strconv.Itoa(egressAllowedProbePort)
	allowedConnect := "exec 3<>/dev/tcp/" + egressAllowedProbeHost + "/" + strconv.Itoa(egressAllowedProbePort)
	return "export LC_ALL=C\n" +
		"outgoing=$(ufw status verbose | sed -n 's/^Default:.*[[:space:]]\\([a-z]*\\) (outgoing).*/\\1/p')\n" +
		"case \"${outgoing}\" in\n" +
		"  deny|reject) ;;\n" +
		"  *) printf '" + LockdownFailedPrefix + "ufw default outgoing is %s\\n' \"${outgoing:-unset (ufw inactive?)}\" >&2; exit 1 ;;\n" +
		"esac\n" +
		"if timeout " + strconv.Itoa(EgressBlockedProbeTimeoutSeconds) + " bash -c " + shell.Quote(connect) + " 2>/dev/null; then\n" +
		"  printf '%s\\n' " + shell.Quote(LockdownFailedPrefix+"tcp "+target+" connected") + " >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"attempt=1\n" +
		"until timeout " + strconv.Itoa(egressAllowedProbeTimeoutSeconds) + " bash -c " + shell.Quote(allowedConnect) + " 2>/dev/null; do\n" +
		"  if [ \"${attempt}\" -ge " + strconv.Itoa(egressAllowedProbeAttempts) + " ]; then\n" +
		"    printf '%s\\n' " + shell.Quote(LockdownFailedPrefix+"tcp "+allowed+" did not connect; the allow list blocks required egress") + " >&2\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  attempt=$((attempt + 1))\n" +
		"  sleep " + strconv.Itoa(egressAllowedProbeRetrySeconds) + "\n" +
		"done\n" +
		"printf 'egress lockdown in force: ufw default outgoing %s; tcp %s blocked; tcp %s allowed\\n' \"${outgoing}\" " + shell.Quote(target) + " " + shell.Quote(allowed) + "\n"
}
