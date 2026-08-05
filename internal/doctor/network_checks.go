package doctor

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"time"
)

func publicSSHClosed(ctx context.Context, ip string) Result {
	if _, err := netip.ParsePrefix(ip); err == nil {
		return skip("network", "public ssh", ip+" tcp/22 skipped: CIDR prefix is not a host address")
	}
	if addr, err := netip.ParseAddr(ip); err == nil && addr.IsUnspecified() {
		return skip("network", "public ssh", ip+" tcp/22 skipped: not a public host address")
	}
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip, "22"))
	if err != nil {
		return publicSSHProbeError(ip, err)
	}
	_ = conn.Close()
	return fail("network", "public ssh", ip+" tcp/22 open", "close provider firewall/UFW SSH ingress")
}

func publicSSHProbeError(ip string, err error) Result {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return pass("network", "public ssh", ip+" tcp/22 closed")
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return pass("network", "public ssh", ip+" tcp/22 unreachable")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return pass("network", "public ssh", ip+" tcp/22 filtered (timeout)")
	}
	return warn("network", "public ssh", ip+" tcp/22 probe inconclusive: "+trim(err.Error()))
}
