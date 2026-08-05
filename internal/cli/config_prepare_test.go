package cli

import (
	"strings"
	"testing"
)

func TestLoadOrCreateConfigNonInteractiveRequiresProject(t *testing.T) {
	a := &app{nonInteractive: true}
	_, exists, err := a.loadOrCreateConfig("", "")
	if err == nil || !strings.Contains(err.Error(), "managed config missing") {
		t.Fatalf("expected managed config error, exists=%v err=%v", exists, err)
	}
	if exists {
		t.Fatal("missing config reported as existing")
	}
}
