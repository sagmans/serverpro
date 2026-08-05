package doctor

import (
	"fmt"
	"net/netip"
	"strings"
)

func summarizeListeningPorts(text string) string {
	lines := nonEmptyLines(text)
	if len(lines) == 0 {
		return "0 listening sockets"
	}
	counts := listeningPortCounts{}
	for _, line := range lines {
		counts.add(listeningLocalHost(line))
	}
	return trim(fmt.Sprintf("%d listening sockets; public_bind=%d loopback=%d private=%d other=%d", len(lines), counts.public, counts.loopback, counts.private, counts.other))
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

type listeningPortCounts struct {
	public   int
	loopback int
	private  int
	other    int
}

func (c *listeningPortCounts) add(host string) {
	switch classifyListeningHost(host) {
	case "public":
		c.public++
	case "loopback":
		c.loopback++
	case "private":
		c.private++
	default:
		c.other++
	}
}

func listeningLocalHost(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return ""
	}
	local := fields[4]
	if strings.HasPrefix(local, "[") {
		end := strings.Index(local, "]")
		if end > 0 {
			return strings.TrimSpace(local[1:end])
		}
	}
	if i := strings.LastIndex(local, ":"); i >= 0 {
		local = local[:i]
	}
	if i := strings.LastIndex(local, "%"); i >= 0 {
		local = local[:i]
	}
	return strings.TrimSpace(local)
}

func classifyListeningHost(host string) string {
	switch strings.TrimSpace(host) {
	case "", "*", "0.0.0.0", "::":
		return "public"
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "other"
	}
	if addr.IsLoopback() {
		return "loopback"
	}
	if addr.IsPrivate() || addr.IsLinkLocalUnicast() || isTailscaleCGNAT(addr) {
		return "private"
	}
	return "public"
}

func isTailscaleCGNAT(addr netip.Addr) bool {
	prefix := netip.MustParsePrefix("100.64.0.0/10")
	return addr.Is4() && prefix.Contains(addr)
}
