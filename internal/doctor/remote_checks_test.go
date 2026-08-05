package doctor

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
)

type batchRemote struct {
	batchCalls     int
	runCalls       int
	commands       []remote.BatchCommand
	runScripts     []string
	results        []remote.BatchResult
	outputByScript map[string]string
	failScript     string
	failError      error
	err            error
}

func (r *batchRemote) Run(_ context.Context, _, _, script string) (string, error) {
	r.runCalls++
	r.runScripts = append(r.runScripts, script)
	return "ok", nil
}

func (r *batchRemote) RunBatch(_ context.Context, _, _ string, commands []remote.BatchCommand) ([]remote.BatchResult, error) {
	r.batchCalls++
	r.commands = append([]remote.BatchCommand(nil), commands...)
	if r.err != nil {
		return nil, r.err
	}
	if r.results != nil {
		return r.results, nil
	}
	results := make([]remote.BatchResult, len(commands))
	for i, command := range commands {
		results[i].Output = "ok"
		if output, ok := r.outputByScript[command.Script]; ok {
			results[i].Output = output
		}
		if command.Script == r.failScript {
			results[i].Err = r.failError
			if results[i].Err == nil {
				results[i].Err = errors.New("read-only check failed")
			}
		}
	}
	return results, nil
}

func TestRemoteChecksBatchReadOnlyCommandsOnce(t *testing.T) {
	cfg := config.Example("prod")
	want := remoteChecksWithOptions(context.Background(), cfg, &fakeRemote{}, "prod-01", Options{})
	runner := &batchRemote{}
	got := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{})
	if runner.batchCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("remote calls batch=%d sequential=%d", runner.batchCalls, runner.runCalls)
	}
	if len(runner.commands) == 0 {
		t.Fatal("batch contained no remote checks")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batched results differ\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestRemoteChecksBatchCollectionHasNoLogSideEffect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Example("prod")
	runner := &batchRemote{}
	_ = remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{})
	if _, err := os.Stat(cloudInitStatusLogPath(cfg)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("batch collection wrote cloud-init detail log: %v", err)
	}
}

func TestRemoteChecksBatchPreservesCloudInitWarningDetail(t *testing.T) {
	cfg := config.Example("prod")
	runner := &batchRemote{
		failScript: cloudInitWaitCommand,
		failError:  errors.New("remote batch command 1 failed with status 2"),
		outputByScript: map[string]string{
			cloudInitWaitCommand: "status: done",
			cloudInitLongCommand: "status: done\nrecoverable errors:\n - package update failed",
		},
	}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "cloud-init", Warn, "package update failed") {
		t.Fatalf("cloud-init batch warning = %+v", results)
	}
	if runner.batchCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("remote calls batch=%d sequential=%d", runner.batchCalls, runner.runCalls)
	}
}

func TestRemoteChecksBatchDelegatesFixAndRecheck(t *testing.T) {
	cfg := config.Example("prod")
	check := sshdSettingValueCommand(sshdKeywordAllowAgentForwarding, sshdValueDisabled)
	runner := &batchRemote{failScript: check}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{Fix: true})
	if !hasResult(Report{Results: results}, "sshd agent forwarding", Pass, "fixed") {
		t.Fatalf("missing fixed batch result: %+v", results)
	}
	if runner.batchCalls != 1 || runner.runCalls != 4 {
		t.Fatalf("remote calls batch=%d sequential=%d scripts=%+v", runner.batchCalls, runner.runCalls, runner.runScripts)
	}
	if !hasCommand(runner.runScripts, "systemctl restart ssh") ||
		!hasCommand(runner.runScripts, check) ||
		!hasCommand(runner.runScripts, bootstraptools.ManagedPackageRefreshCommand()) {
		t.Fatalf("fix, package refresh, and rechecks not delegated: %+v", runner.runScripts)
	}
}

func TestRemoteChecksBatchAllowsManagedPackageRefresh(t *testing.T) {
	cfg := config.Example("prod")
	packageCheck := remoteToolCheckByName(t, bootstraptools.Checks(cfg.Admin.Username), bootstraptools.ManagedPackageCheckName)
	runner := &batchRemote{outputByScript: map[string]string{packageCheck.Command: "current"}}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{Fix: true})
	if !hasResult(Report{Results: results}, bootstraptools.ManagedPackageCheckName, Pass, "fixed") {
		t.Fatalf("batched package refresh result = %+v", results)
	}
	if !hasCommand(runner.runScripts, bootstraptools.ManagedPackageRefreshCommand()) {
		t.Fatalf("batched package refresh did not reach live runner: %+v", runner.runScripts)
	}
}

func TestRemoteChecksBatchCommandOutputOverflowDoesNotAttemptFixes(t *testing.T) {
	cfg := config.Example("prod")
	check := "ufw status verbose | grep -Fx 'Status: active'"
	runner := &batchRemote{failScript: check, failError: &remote.BatchCommandOutputLimitError{Index: 4, Limit: 8}}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{Fix: true})
	if runner.batchCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("overflow attempted fix: batch=%d sequential=%d scripts=%+v", runner.batchCalls, runner.runCalls, runner.runScripts)
	}
	if !hasResult(Report{Results: results}, "ufw active", Fail, "output exceeds") {
		t.Fatalf("overflow result = %+v", results)
	}
}

func TestRemoteChecksBatchTransportFailureDoesNotAttemptFixes(t *testing.T) {
	cfg := config.Example("prod")
	runner := &batchRemote{err: errors.New("transport unavailable")}
	results := remoteChecksWithOptions(context.Background(), cfg, runner, "prod-01", Options{Fix: true})
	if runner.batchCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("remote calls batch=%d sequential=%d", runner.batchCalls, runner.runCalls)
	}
	if len(results) == 0 || results[0].Status != Fail || !strings.Contains(results[0].Evidence, "transport unavailable") {
		t.Fatalf("transport failure results = %+v", results)
	}
}

func TestRemoteChecksIncludesRestrictedEgressWarning(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Egress.Mode = "restricted"
	results := remoteChecksWithOptions(context.Background(), cfg, &fakeRemote{}, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "egress precision", Warn, "best-effort") {
		t.Fatalf("missing restricted egress warning: %+v", results)
	}
}

func TestRemoteChecksSkipsCloudflaredWhenNoIngress(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "none"
	r := &scriptedRemote{responses: map[string][]remoteCall{}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	for _, res := range results {
		if res.Name == "cloudflared" {
			t.Fatalf("cloudflared check should be skipped when ingress=none: %+v", results)
		}
	}
}

func TestRemoteChecksIncludesCloudflaredWhenIngressConfigured(t *testing.T) {
	cfg := config.Example("prod")
	cfg.Network.Ingress = "cloudflare-tunnel"
	r := &scriptedRemote{responses: map[string][]remoteCall{
		"systemctl is-active cloudflared": {{out: "active\n"}},
	}}
	results := remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	if !hasResult(Report{Results: results}, "cloudflared", Pass, "active") {
		t.Fatalf("expected cloudflared pass: %+v", results)
	}
}

func TestRemoteChecksEgressCommandUsesReliableEndpoints(t *testing.T) {
	cfg := config.Example("prod")
	r := &scriptedRemote{responses: map[string][]remoteCall{}}
	_ = remoteChecksWithOptions(context.Background(), cfg, r, "prod-01", Options{})
	for _, call := range r.commands {
		if strings.Contains(call, "www.cloudflare.com") {
			t.Fatalf("egress check should not use www.cloudflare.com: %q", call)
		}
		if strings.Contains(call, "1.1.1.1") {
			return
		}
	}
	t.Fatal("egress check should include 1.1.1.1")
}
