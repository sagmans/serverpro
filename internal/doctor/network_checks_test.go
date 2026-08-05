package doctor

import (
	"context"
	"strings"
	"testing"
)

type timeoutProbeError struct{}

func (timeoutProbeError) Error() string   { return "i/o timeout" }
func (timeoutProbeError) Timeout() bool   { return true }
func (timeoutProbeError) Temporary() bool { return true }

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
