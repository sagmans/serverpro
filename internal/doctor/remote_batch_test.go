package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/tailscaletools"
	"github.com/sagmans/serverpro/internal/tunnel"
)

func TestBatchReplayRunWithInputDelegatesOnlyPlannedFix(t *testing.T) {
	const fixCommand = "planned fix"
	plan := remoteReadPlan{liveCommands: commandSet(fixCommand)}
	source := &scriptedRemote{responses: map[string][]remoteCall{fixCommand: {{out: "fixed"}}}}
	replay := newBatchReplayRunner(source, plan, nil)
	out, err := replay.RunWithInput(context.Background(), "deploy", "prod-01", fixCommand, "protected")
	if err != nil || out != "fixed" {
		t.Fatalf("RunWithInput output=%q err=%v", out, err)
	}
	if len(source.inputs) != 1 || source.inputs[0] != "protected" {
		t.Fatalf("protected inputs = %+v", source.inputs)
	}
	if _, err := replay.RunWithInput(context.Background(), "deploy", "prod-01", "unplanned fix", "protected"); !errors.Is(err, errUnplannedRemoteCommand) {
		t.Fatalf("unplanned protected-input command error = %v", err)
	}
	if len(source.inputs) != 1 {
		t.Fatalf("unplanned fix reached source: %+v", source.inputs)
	}

	unsupported := newBatchReplayRunner(&fakeRemote{}, plan, nil)
	if _, err := unsupported.RunWithInput(context.Background(), "deploy", "prod-01", fixCommand, "protected"); err == nil || !strings.Contains(err.Error(), "protected stdin") {
		t.Fatalf("missing unsupported-input error: %v", err)
	}
}

func TestBatchReplayRejectsUnplannedReadWithoutSourceFallback(t *testing.T) {
	source := &scriptedRemote{responses: map[string][]remoteCall{}}
	replay := newBatchReplayRunner(source, remoteReadPlan{}, nil)
	if _, err := replay.Run(context.Background(), "deploy", "prod-01", "unplanned read"); !errors.Is(err, errUnplannedRemoteCommand) {
		t.Fatalf("unplanned read error = %v", err)
	}
	if len(source.commands) != 0 {
		t.Fatalf("unplanned read reached source: %+v", source.commands)
	}
}

func TestRemoteReadPlanDeclaresConditionalAndConfiguredReads(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "none"
	plan := buildRemoteReadPlan(cfg)
	if !plan.hasRead(cloudInitWaitCommand) || !plan.hasRead(cloudInitLongCommand) {
		t.Fatalf("cloud-init reads missing from plan: %+v", plan.commands)
	}
	if plan.hasRead(tunnel.CheckCommand()) {
		t.Fatalf("cloudflared read planned without ingress: %+v", plan.commands)
	}

	cfg.Network.Ingress = "cloudflare-tunnel"
	plan = buildRemoteReadPlan(cfg)
	if !plan.hasRead(tunnel.CheckCommand()) {
		t.Fatalf("cloudflared read missing with ingress: %+v", plan.commands)
	}
	if !plan.hasRead(tailscaletools.CheckCommand()) {
		t.Fatal("Tailscale version read missing from plan")
	}
	for _, fix := range []string{sudoPasswordFixCommand(cfg.Admin.Username), sshdSettingsFixCommand(), tailscaletools.UpdateScript()} {
		if !plan.allowsLive(fix) {
			t.Fatalf("fix missing from live plan: %q", fix)
		}
	}
}
