package doctor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func TestRemoteCheckSpecificationsOwnSequentialAndBatchReadOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Example("prod")
	cfg.Network.Ingress = "cloudflare-tunnel"
	runner := &scriptedRemote{responses: map[string][]remoteCall{
		cloudInitWaitCommand: {{out: "status: done", err: errors.New("exit status 2")}},
	}}
	_ = remoteChecksSequential(context.Background(), cfg, runner, "prod-01", Options{})
	plan := buildRemoteReadPlan(cfg)
	planned := make([]string, len(plan.commands))
	for i, command := range plan.commands {
		planned[i] = command.Script
	}
	if !reflect.DeepEqual(runner.commands, planned) {
		t.Fatalf("sequential reads differ from batch authority\nsequential: %+v\nplanned:    %+v", runner.commands, planned)
	}
}

func TestRemoteCheckSpecificationsDeclareEveryLiveFix(t *testing.T) {
	cfg := config.Example("prod")
	plan := buildRemoteReadPlan(cfg)
	for _, specification := range remoteCheckSpecifications(cfg) {
		for _, command := range specification.liveCommands {
			if !plan.allowsLive(command) {
				t.Fatalf("live fix missing from batch authority: %q", command)
			}
		}
	}
}
