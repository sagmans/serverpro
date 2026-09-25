package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/config"
)

const (
	diagnosticCheckError  = "remote batch command 24 failed with status 1"
	diagnosticCheckOutput = "managed package updates available"
	diagnosticRepairError = "tailscale ssh failed: exit status 100"
	diagnosticAptCause    = "E: Unable to correct problems, you have held broken packages."
	diagnosticLogLines    = 20
)

func TestManagedPackageFailureIncludesCapturedOutput(t *testing.T) {
	for _, batched := range []bool{false, true} {
		name := "sequential"
		if batched {
			name = "batched"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.Example("fixture")
			check := remoteToolCheckByName(t, bootstraptools.Checks(cfg.Admin.Username), bootstraptools.ManagedPackageCheckName)
			source := &scriptedRemote{responses: map[string][]remoteCall{
				check.Command: {{out: diagnosticAptCause, err: errors.New(diagnosticCheckError)}},
			}}
			var results []Result
			if batched {
				batch := &batchRemote{failScript: check.Command, failError: errors.New(diagnosticCheckError), outputByScript: map[string]string{check.Command: diagnosticAptCause}}
				results = remoteChecksBatched(context.Background(), cfg, source, batch, "fixture-host", Options{})
			} else {
				results = remoteChecksSequential(context.Background(), cfg, source, "fixture-host", Options{})
			}
			result := writtenPackageResult(t, Report{Results: results})
			if result.Status != Fail || !strings.Contains(result.Evidence, diagnosticAptCause) || !strings.Contains(result.Evidence, diagnosticCheckError) {
				t.Fatalf("package failure lost captured output: %+v", result)
			}
			if hasCommand(source.commands, "serverpro-bootstrap-tools") {
				t.Fatal("read-only diagnostics attempted repair")
			}
		})
	}
}

func TestManagedPackageRepairFailureKeepsCause(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		name := "separate-output"
		if embedded {
			name = "output-in-error"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.Example("fixture")
			check := remoteToolCheckByName(t, bootstraptools.Checks(cfg.Admin.Username), bootstraptools.ManagedPackageCheckName)
			output := strings.Repeat("[serverpro-bootstrap-tools] package progress\n", diagnosticLogLines) + diagnosticAptCause
			message := diagnosticRepairError
			if embedded {
				message += ": " + output
			}
			source := &scriptedRemote{responses: map[string][]remoteCall{
				bootstraptools.InstallScriptForUser(cfg.Admin.Username): {{out: output, err: errors.New(message)}},
			}}
			batch := &batchRemote{failScript: check.Command, failError: errors.New(diagnosticCheckError), outputByScript: map[string]string{check.Command: diagnosticCheckOutput}}
			results := remoteChecksBatched(context.Background(), cfg, source, batch, "fixture-host", Options{Fix: true})
			result := writtenPackageResult(t, Report{Results: results})
			if result.Status != Fail || result.Remediation != "inspect remote command" {
				t.Fatalf("repair failure changed status or remediation: %+v", result)
			}
			for _, want := range []string{diagnosticCheckError, diagnosticCheckOutput, "fix failed: " + diagnosticRepairError, diagnosticAptCause} {
				if strings.Count(result.Evidence, want) != 1 {
					t.Errorf("evidence must retain %q once: %q", want, result.Evidence)
				}
			}
		})
	}
}

func writtenPackageResult(t *testing.T, report Report) Result {
	t.Helper()
	var output bytes.Buffer
	if err := report.Write(&output); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, result := range decoded.Results {
		if result.Name == bootstraptools.ManagedPackageCheckName {
			return result
		}
	}
	t.Fatal("managed package result missing")
	return Result{}
}
