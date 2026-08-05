package doctor

import "testing"

func TestSummarizeListeningPortsCountsPublicBinds(t *testing.T) {
	out := summarizeListeningPorts("tcp LISTEN 0 4096 0.0.0.0:80 0.0.0.0:*\ntcp LISTEN 0 4096 [::]:443 [::]:*\ntcp LISTEN 0 4096 198.51.100.10:8080 0.0.0.0:*")
	if out != "3 listening sockets; public_bind=3 loopback=0 private=0 other=0" {
		t.Fatalf("bad public bind summary: %s", out)
	}
}
