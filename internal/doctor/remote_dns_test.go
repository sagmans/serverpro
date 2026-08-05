package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestRemoteCheckSpecificationsIncludeDNSResolutionCanary(t *testing.T) {
	cfg := config.Example("prod")
	found := false
	for _, specification := range remoteCheckSpecifications(cfg) {
		for _, command := range specification.readCommands {
			if strings.Contains(command, dnsCanaryName) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("dns resolution canary missing from remote check specifications")
	}
}

func TestRemoteDNSResolutionFailureCarriesGuidance(t *testing.T) {
	cfg := config.Example("prod")
	var specification remoteCheckSpecification
	for _, candidate := range remoteCheckSpecifications(cfg) {
		for _, command := range candidate.readCommands {
			if strings.Contains(command, dnsCanaryName) {
				specification = candidate
			}
		}
	}
	runner := &fakeRemote{errs: map[string]error{specification.readCommands[0]: errors.New("exit status 2")}}
	results := specification.run(context.Background(), runner, cfg.Admin.Username, "prod-01", Options{})
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	result := results[0]
	if result.Status != Fail || result.Scope != "remote" || result.Name != "dns resolution" {
		t.Fatalf("bad result: %+v", result)
	}
	if !strings.Contains(result.Remediation, "tailnet") {
		t.Fatalf("remediation lacks DNS guidance: %+v", result)
	}
}
