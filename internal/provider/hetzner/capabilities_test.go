package hetzner

import (
	"context"
	"testing"
)

// WHY: the CLI advertises provider capabilities in `provider status`. Pin the
// Hetzner adapter's declared name and capability set so the facade contract
// cannot regress silently.

func TestComputeProviderNameAndCapabilities(t *testing.T) {
	p := NewComputeProvider(nil)
	if p.Name() != "hetzner" {
		t.Fatalf("name = %q", p.Name())
	}
	caps := p.Capabilities(context.Background())
	if !caps.CreateServer || !caps.DeleteServer || !caps.PowerServer || !caps.Catalog || !caps.ListServers {
		t.Fatalf("capabilities = %+v", caps)
	}
}
