package doctor

import (
	"context"
	"net"
	"strings"
	"testing"
)

type trackingConn struct {
	net.Conn
	closed bool
}

func (c *trackingConn) Close() error {
	c.closed = true
	return c.Conn.Close()
}

type timeoutProbeError struct{}

func (timeoutProbeError) Error() string   { return "i/o timeout" }
func (timeoutProbeError) Timeout() bool   { return true }
func (timeoutProbeError) Temporary() bool { return true }

func TestPublicSSHProbeClosesSuccessfulConnection(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	conn := &trackingConn{Conn: client}
	err := publicSSHProbeWithDialer(context.Background(), "203.0.113.10", func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "203.0.113.10:22" {
			t.Fatalf("network=%q address=%q", network, address)
		}
		return conn, nil
	})
	if err != nil || !conn.closed {
		t.Fatalf("error=%v closed=%t", err, conn.closed)
	}
}

func TestPublicSSHClosedWarnsOnInconclusiveProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := publicSSHClosed(ctx, "127.0.0.1")
	if res.Status != Warn || !strings.Contains(res.Evidence, "probe inconclusive") {
		t.Fatalf("expected inconclusive warning, got %+v", res)
	}
}

func TestPublicSSHClosedPassesOnTimeout(t *testing.T) {
	res := publicSSHProbeError("203.0.113.10", timeoutProbeError{})
	if res.Status != Pass || !strings.Contains(res.Evidence, "filtered") {
		t.Fatalf("expected filtered pass, got %+v", res)
	}
}

func TestPublicSSHClosedSkipsCIDRPrefix(t *testing.T) {
	res := publicSSHClosed(context.Background(), "2001:db8::/64")
	if res.Status != Skip || !strings.Contains(res.Evidence, "CIDR prefix") {
		t.Fatalf("expected CIDR prefix skip, got %+v", res)
	}
}

func TestPublicSSHClosedSkipsUnspecifiedHosts(t *testing.T) {
	for _, ip := range []string{"0.0.0.0", "::"} {
		t.Run(ip, func(t *testing.T) {
			res := publicSSHClosed(context.Background(), ip)
			if res.Status != Skip || !strings.Contains(res.Evidence, "not a public host address") {
				t.Fatalf("expected unspecified host skip, got %+v", res)
			}
		})
	}
}
