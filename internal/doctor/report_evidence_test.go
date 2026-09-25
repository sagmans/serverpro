package doctor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sagmans/serverpro/internal/bootstraptools"
)

const (
	testFailureEvidenceLimit = 4096
	testFailureHeadLimit     = 1024
	testOldEvidenceLimit     = 160
	testDiagnosticSecret     = "fixture-private-credential-not-for-output"
	testSecretOverlap        = 8
	testDiagnosticNoise      = "package progress\n"
	testDiagnosticNoiseLines = 600
)

func TestFailureEvidenceRedactsBeforeBounding(t *testing.T) {
	for _, boundary := range []int{testOldEvidenceLimit, testFailureHeadLimit, testFailureEvidenceLimit} {
		evidence := strings.Repeat("p", boundary-testSecretOverlap) + testDiagnosticSecret + "\n" + strings.Repeat(testDiagnosticNoise, testDiagnosticNoiseLines) + diagnosticAptCause
		report := Report{Results: []Result{fail("remote", bootstraptools.ManagedPackageCheckName, evidence, "inspect remote command")}}.Redact(testDiagnosticSecret)
		before := report.Results[0].Evidence
		result := writtenPackageResult(t, report)
		if strings.Contains(result.Evidence, testDiagnosticSecret[:testSecretOverlap]) {
			t.Errorf("credential fragment leaked at boundary %d", boundary)
		}
		if !strings.HasSuffix(result.Evidence, diagnosticAptCause) {
			t.Errorf("terminal cause lost at boundary %d", boundary)
		}
		if len(result.Evidence) > testFailureEvidenceLimit {
			t.Errorf("failure evidence grew to %d bytes", len(result.Evidence))
		}
		if report.Results[0].Evidence != before {
			t.Fatal("writing the report mutated evidence needed for later redaction")
		}
	}
}

func TestFailureEvidenceKeepsContextAndTail(t *testing.T) {
	evidence := diagnosticCheckError + "; fix failed: " + diagnosticRepairError + "\n" + strings.Repeat("更新中\n", testDiagnosticNoiseLines) + diagnosticAptCause
	report := Report{Results: []Result{fail("remote", bootstraptools.ManagedPackageCheckName, evidence, "inspect remote command")}}
	result := writtenPackageResult(t, report.Redact())
	if !strings.HasPrefix(result.Evidence, diagnosticCheckError) || !strings.Contains(result.Evidence, diagnosticRepairError) || !strings.HasSuffix(result.Evidence, diagnosticAptCause) {
		t.Fatalf("bounded failure lost context or terminal cause: %q", result.Evidence)
	}
	if len(result.Evidence) > testFailureEvidenceLimit || !utf8.ValidString(result.Evidence) || strings.ContainsRune(result.Evidence, utf8.RuneError) {
		t.Fatalf("bounded evidence must retain valid UTF-8 within %d bytes", testFailureEvidenceLimit)
	}
}

func TestFailureEvidenceRedactsTailBoundary(t *testing.T) {
	const marker = "\n... [truncated] ...\n"
	tailBytes := testFailureEvidenceLimit - testFailureHeadLimit - len(marker)
	secretTail := testDiagnosticSecret[testSecretOverlap:]
	evidence := strings.Repeat(testDiagnosticNoise, testDiagnosticNoiseLines) + testDiagnosticSecret + strings.Repeat("z", tailBytes-len(secretTail))
	report := Report{Results: []Result{fail("remote", bootstraptools.ManagedPackageCheckName, evidence, "inspect remote command")}}.Redact(testDiagnosticSecret)
	result := writtenPackageResult(t, report)
	if strings.Contains(result.Evidence, secretTail) {
		t.Fatal("credential suffix leaked at the retained tail boundary")
	}
	if !strings.Contains(result.Evidence, marker) || !strings.HasSuffix(result.Evidence, strings.Repeat("z", testSecretOverlap)) {
		t.Fatal("failure evidence did not retain the diagnostic tail")
	}
}

func TestReportWriteKeepsShortEvidence(t *testing.T) {
	want := Report{Results: []Result{
		pass("remote", "healthy", "current"),
		fail("remote", "unhealthy", diagnosticAptCause, "inspect remote command"),
	}}
	var output bytes.Buffer
	if err := want.Write(&output); err != nil {
		t.Fatal(err)
	}
	var got Report
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != len(want.Results) {
		t.Fatalf("result count = %d, want %d", len(got.Results), len(want.Results))
	}
	for i := range want.Results {
		if got.Results[i] != want.Results[i] {
			t.Fatalf("short result changed: got %+v, want %+v", got.Results[i], want.Results[i])
		}
	}
}
