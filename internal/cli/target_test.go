package cli

import (
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func TestValidateStateTargetRejectsResourceNameMismatches(t *testing.T) {
	cfg := config.ExampleServer("prod", "web")

	serverNameErr := validateStateTarget(cfg, state.State{Project: "prod", Server: "web", Compute: state.ComputeState{Name: "prod-api"}})
	if serverNameErr == nil || !strings.Contains(serverNameErr.Error(), "state server name") {
		t.Fatalf("expected server name mismatch, got %v", serverNameErr)
	}

	tunnelNameErr := validateStateTarget(cfg, state.State{Project: "prod", Server: "web", Cloudflare: state.CloudflareState{Name: "prod-api"}})
	if tunnelNameErr == nil || !strings.Contains(tunnelNameErr.Error(), "state tunnel name") {
		t.Fatalf("expected tunnel name mismatch, got %v", tunnelNameErr)
	}
}
