package doctor

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

const (
	// listenerNetidTCP is ss's Netid for TCP sockets; serve backends are TCP.
	listenerNetidTCP = "tcp"
	// tailscaleServeRemediation covers both findings: Funnel publishes a port
	// to the internet outside config ingress, and a backend with no listener
	// hands its tailnet hostname to whatever process binds that port next.
	tailscaleServeRemediation = "remove unused entries (sudo tailscale serve reset, or tailscale serve --https=PORT off); turn Funnel off (sudo tailscale funnel reset) unless public ingress is intended, since config ingress does not cover it"
)

// serveConfig is the part of tailscale's ipn.ServeConfig JSON that names
// backends and Funnel. Services and Foreground nest the same shape, so one
// walk covers background, per-session, and service configs alike.
type serveConfig struct {
	TCP         map[string]serveTCPHandler `json:"TCP"`
	Web         map[string]serveWebConfig  `json:"Web"`
	AllowFunnel map[string]bool            `json:"AllowFunnel"`
	Services    map[string]serveConfig     `json:"Services"`
	Foreground  map[string]serveConfig     `json:"Foreground"`
}

type serveTCPHandler struct {
	TCPForward string `json:"TCPForward"`
}

type serveWebConfig struct {
	Handlers map[string]serveHTTPHandler `json:"Handlers"`
}

// serveHTTPHandler keeps only Proxy: Path and Text handlers serve files or a
// fixed string from tailscaled itself and have no backend port to check.
type serveHTTPHandler struct {
	Proxy string `json:"Proxy"`
}

// tailscaleServeResult fails on Funnel, warns on a local backend whose port
// has no TCP listener, and passes otherwise. Backends on other hosts are
// skipped: a local listener table cannot vouch for them.
func tailscaleServeResult(out string) Result {
	serveJSON, listeners, ok := splitServeOutput(out)
	if !ok {
		return fail("remote", tailscaleServeCheckName, "unexpected serve probe output: "+trim(out), driftProbeFailedRemediation)
	}
	var config serveConfig
	if strings.TrimSpace(serveJSON) != "" {
		if err := json.Unmarshal([]byte(serveJSON), &config); err != nil {
			return fail("remote", tailscaleServeCheckName, "cannot parse tailscale serve status --json: "+err.Error(), driftProbeFailedRemediation)
		}
	}
	funnels, backends := config.collect()
	listening := tcpListenerPorts(listeners)
	var findings []string
	for _, hostPort := range funnels {
		findings = append(findings, "Funnel publishes "+hostPort+" to the internet")
	}
	for _, backend := range backends {
		if !listening[backend.port] {
			findings = append(findings, fmt.Sprintf("%s forwards to %s, but nothing listens on port %s", backend.entry, backend.target, backend.port))
		}
	}
	switch {
	case len(funnels) > 0:
		return fail("remote", tailscaleServeCheckName, strings.Join(findings, driftEvidenceSeparator), tailscaleServeRemediation)
	case len(findings) > 0:
		result := warn("remote", tailscaleServeCheckName, strings.Join(findings, driftEvidenceSeparator))
		result.Remediation = tailscaleServeRemediation
		return result
	case len(backends) == 0:
		return pass("remote", tailscaleServeCheckName, "no serve entries")
	}
	return pass("remote", tailscaleServeCheckName, fmt.Sprintf("%d local serve backends listening", len(backends)))
}

// splitServeOutput separates the two documents TailscaleServeCommand prints.
func splitServeOutput(out string) (string, string, bool) {
	_, rest, ok := strings.Cut(out, tailscaleServeSectionMarker+"\n")
	if !ok {
		return "", "", false
	}
	serveJSON, listeners, ok := strings.Cut(rest, listenersSectionMarker)
	return serveJSON, listeners, ok
}

type serveBackend struct {
	entry  string
	target string
	port   string
}

// collect returns sorted Funnel host:ports and local backends so evidence is
// deterministic across runs despite Go's random map order.
func (c serveConfig) collect() ([]string, []serveBackend) {
	var funnels []string
	var backends []serveBackend
	for hostPort, on := range c.AllowFunnel {
		if on {
			funnels = append(funnels, hostPort)
		}
	}
	for port, handler := range c.TCP {
		if backend, ok := localServeBackend("tcp "+port, handler.TCPForward); ok {
			backends = append(backends, backend)
		}
	}
	for hostPort, web := range c.Web {
		for path, handler := range web.Handlers {
			if backend, ok := localServeBackend(hostPort+path, handler.Proxy); ok {
				backends = append(backends, backend)
			}
		}
	}
	for _, nested := range []map[string]serveConfig{c.Services, c.Foreground} {
		for _, child := range nested {
			childFunnels, childBackends := child.collect()
			funnels = append(funnels, childFunnels...)
			backends = append(backends, childBackends...)
		}
	}
	sort.Strings(funnels)
	sort.Slice(backends, func(i, j int) bool { return backends[i].entry < backends[j].entry })
	return funnels, backends
}

// localServeBackend extracts the port of a loopback backend. Tailscale stores
// proxies as URLs ("http://127.0.0.1:3000") and TCP forwards as host:port.
func localServeBackend(entry, target string) (serveBackend, bool) {
	if target == "" {
		return serveBackend{}, false
	}
	hostPort := target
	shown := target
	if strings.Contains(target, "://") {
		parsed, err := url.Parse(target)
		if err != nil || parsed.Port() == "" {
			return serveBackend{}, false
		}
		hostPort = parsed.Host
		shown = serveTargetEvidence(parsed)
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil || !isLoopbackServeHost(host) {
		return serveBackend{}, false
	}
	return serveBackend{entry: entry, target: shown, port: port}, true
}

// serveTargetEvidence keeps only the scheme, host, port, and path of a proxy
// URL. Evidence is printed and can be shared, while userinfo, a query, or a
// fragment can carry a backend credential; the finding needs only the port.
func serveTargetEvidence(target *url.URL) string {
	shown := url.URL{Scheme: target.Scheme, Host: target.Host, Path: target.Path, RawPath: target.RawPath}
	return shown.String()
}

func isLoopbackServeHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// tcpListenerPorts lists ports with a TCP listener a loopback backend can
// reach: loopback or wildcard binds. A socket bound only to a tailnet or
// public address does not answer on 127.0.0.1.
func tcpListenerPorts(text string) map[string]bool {
	ports := map[string]bool{}
	for _, line := range nonEmptyLines(text) {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != listenerNetidTCP {
			continue
		}
		host := listeningLocalHost(line)
		if !isLoopbackServeHost(host) && host != "*" && host != "0.0.0.0" && host != "::" {
			continue
		}
		if i := strings.LastIndex(fields[4], ":"); i >= 0 {
			ports[fields[4][i+1:]] = true
		}
	}
	return ports
}
