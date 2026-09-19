//go:build serverpro_full_chain_e2e

package e2e_test

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/bootstraptools"
	"github.com/sagmans/serverpro/internal/doctor"
)

const (
	compiledDoctorFailureEnv       = "SERVERPRO_E2E_DOCTOR_PACKAGE_FAILURE"
	compiledDoctorShortScenario    = "short"
	compiledDoctorBoundaryScenario = "boundary"
	compiledDoctorNamespace        = "e2e-doctor-packages"
	compiledDoctorProvider         = "hetzner"
	compiledDoctorFailureExitCode  = 1
	compiledDoctorMaxEvidenceBytes = 4096
	compiledDoctorBaselineEvidence = "managed package below baseline: synthetic-package 0.1 < 1.0"
	compiledDoctorTerminalCause    = "E: synthetic fixture package transaction rejected"
	compiledDoctorBatchFailure     = "remote batch command "
	compiledDoctorBatchStatus      = "failed with status 1"
	compiledDoctorRepairFailure    = "fix failed: "
	compiledDoctorRepairExitStatus = "exit status 100"
	compiledDoctorTruncation       = "[truncated]"
	compiledDoctorRedaction        = "[REDACTED]"
)

func TestCompiledDoctorPackageDiagnostics(t *testing.T) {
	fixture := newProviderFixture(t)
	binary := buildE2EBinary(t)
	fakeBin := writeFakeTailscale(t)
	home := t.TempDir()
	writeCredentials(t, home, compiledDoctorNamespace)
	env := replaceEnv(journeyEnv(home, fakeBin, fixture.URL(), compiledDoctorNamespace), compiledDoctorFailureEnv, "")
	artifacts := newArtifactLog(t, "doctor-package-diagnostics")

	create := runCommand(binary, env, "server", "create", testServer,
		"--namespace", compiledDoctorNamespace, "--provider", compiledDoctorProvider,
		"--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04",
		"--ingress", "none", "--non-interactive", "--yes")
	artifacts.record("create", create)
	requireSuccessJSON(t, create)

	for _, tc := range []struct {
		name      string
		scenario  string
		fix       bool
		truncated bool
	}{
		{name: "read-only-short", scenario: compiledDoctorShortScenario},
		{name: "read-only-boundary-secret", scenario: compiledDoctorBoundaryScenario, truncated: true},
		{name: "repair-noisy-terminal-cause", scenario: compiledDoctorShortScenario, fix: true, truncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"server", "doctor", testServer,
				"--namespace", compiledDoctorNamespace, "--provider", compiledDoctorProvider, "--non-interactive"}
			if tc.fix {
				args = append(args, "--fix")
			}
			result := runCommand(binary, replaceEnv(env, compiledDoctorFailureEnv, tc.scenario), args...)
			artifacts.record(tc.name, result)
			requireDoctorSecretFreeOutput(t, result)
			var exitErr *exec.ExitError
			if !errors.As(result.err, &exitErr) || exitErr.ExitCode() != compiledDoctorFailureExitCode {
				t.Fatalf("doctor exit = %v, want status %d", result.err, compiledDoctorFailureExitCode)
			}
			if !strings.Contains(result.stderr, "doctor failed") {
				t.Fatalf("doctor failure missing from stderr: %q", result.stderr)
			}
			var report doctor.Report
			if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
				t.Fatalf("doctor stdout is not one JSON report: %v", err)
			}
			if report.Passed() {
				t.Fatal("failed managed package check reported success")
			}
			var packageResult *doctor.Result
			for i := range report.Results {
				check := &report.Results[i]
				if check.Status == doctor.Fail && len(check.Evidence) > compiledDoctorMaxEvidenceBytes {
					t.Errorf("%s evidence is %d bytes, exceeds %d", check.Name, len(check.Evidence), compiledDoctorMaxEvidenceBytes)
				}
				if check.Name == bootstraptools.ManagedPackageCheckName {
					packageResult = check
				}
			}
			if packageResult == nil || packageResult.Status != doctor.Fail || packageResult.Scope != "remote" {
				t.Fatalf("missing failed remote managed package result: %+v", packageResult)
			}
			for _, want := range []string{compiledDoctorBatchFailure, compiledDoctorBatchStatus, compiledDoctorBaselineEvidence, compiledDoctorRedaction} {
				if !strings.Contains(packageResult.Evidence, want) {
					t.Errorf("managed package evidence missing %q: %q", want, packageResult.Evidence)
				}
			}
			for _, want := range []string{compiledDoctorRepairFailure, compiledDoctorRepairExitStatus, compiledDoctorTerminalCause} {
				if strings.Contains(packageResult.Evidence, want) != tc.fix {
					t.Errorf("repair evidence %q present = %v, want %v", want, strings.Contains(packageResult.Evidence, want), tc.fix)
				}
			}
			if strings.Contains(packageResult.Evidence, compiledDoctorTruncation) != tc.truncated {
				t.Errorf("truncation present = %v, want %v", strings.Contains(packageResult.Evidence, compiledDoctorTruncation), tc.truncated)
			}
		})
	}

	remove := runCommand(binary, env, "server", "delete", testServer,
		"--namespace", compiledDoctorNamespace, "--provider", compiledDoctorProvider, "--non-interactive", "--yes")
	artifacts.record("delete", remove)
	requireSuccessJSON(t, remove)
	if remaining := fixture.resourceCount(compiledDoctorProvider); remaining != 0 {
		t.Fatalf("fixture resources remain: %d", remaining)
	}
	requireServerArtifactsAbsent(t, home, compiledDoctorNamespace)
}

func requireDoctorSecretFreeOutput(t *testing.T, result commandResult) {
	t.Helper()
	// Saved failure artifacts scrub secrets separately, so only raw streams prove the CLI contract.
	for _, secret := range []string{testProviderToken, testTailscaleToken, testSudoPassword} {
		fragments := []string{secret, secret[:len(secret)/2], secret[len(secret)/2:]}
		for _, fragment := range fragments {
			if strings.Contains(result.stdout, fragment) || strings.Contains(result.stderr, fragment) {
				t.Fatalf("raw doctor output leaked fixture secret or boundary fragment %q", fragment)
			}
		}
	}
}
