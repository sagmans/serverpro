package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRemoteInventoryReturnsHostSummary(t *testing.T) {
	r := &scriptedRemote{responses: map[string][]remoteCall{
		remoteInventoryCommand(): {{out: "os=Ubuntu 24.04 kernel=6.8 cpu=2 ram_kib=4096000"}},
	}}
	items := remoteInventory(context.Background(), r, "deploy", "prod-01")
	if len(items) != 1 || items[0].Name != "host" || !strings.Contains(items[0].Value, "Ubuntu 24.04") || !strings.Contains(items[0].Value, "ram_kib=4096000") {
		t.Fatalf("bad remote inventory: %+v", items)
	}
}

func TestRemoteInventorySkipsFailedProbe(t *testing.T) {
	r := &scriptedRemote{responses: map[string][]remoteCall{
		remoteInventoryCommand(): {{err: errors.New("sudo password required")}},
	}}
	if items := remoteInventory(context.Background(), r, "deploy", "prod-01"); len(items) != 0 {
		t.Fatalf("expected failed inventory to be skipped, got %+v", items)
	}
}
