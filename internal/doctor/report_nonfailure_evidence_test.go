package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReportRedactionPreservesOperationalIdentity(t *testing.T) {
	report := Report{Results: []Result{{
		Scope: remoteInventoryScope, Name: SudoPasswordCheckName,
		Code: SudoPasswordAuthFailureCode, Status: Fail, Evidence: "remote sudo",
	}}}.Redact("remote", "sudo")
	if report.Results[0].Scope != remoteInventoryScope || !IsSudoPasswordAuthFailure(report.Results[0]) {
		t.Fatal("credential redaction changed metadata required for remote retry")
	}
}

func TestRemoteInventoryLimitSurvivesMetadataRedaction(t *testing.T) {
	report := Report{Inventory: []InventoryItem{{Scope: remoteInventoryScope, Name: remoteInventoryName, Value: strings.Repeat("x", testOldEvidenceLimit*2)}}}.RedactForOutput("remote", "host")
	var output bytes.Buffer
	if err := report.Write(&output); err != nil {
		t.Fatal(err)
	}
	var saved Report
	if err := json.Unmarshal(output.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Inventory[0].Value) > testOldEvidenceLimit+len("...") {
		t.Fatal("metadata masking bypassed remote inventory evidence limit")
	}
}

func TestReportRedactsTextualMetadata(t *testing.T) {
	const secret = testDiagnosticSecret
	report := Report{
		Inventory: []InventoryItem{{Scope: secret, Name: secret, Value: secret}},
		Results:   []Result{{Scope: secret, Name: secret, Code: ResultCode(secret), Status: Warn, Evidence: secret, Remediation: secret}},
	}.RedactForOutput(secret)
	var output bytes.Buffer
	if err := report.Write(&output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("credential leaked through diagnostic metadata")
	}
}

func TestNonFailureEvidenceRedactsBeforeBounding(t *testing.T) {
	const marker = "status:"
	evidence := marker + strings.Repeat("p", testOldEvidenceLimit-testSecretOverlap-len(marker)) + testDiagnosticSecret + strings.Repeat("x", testOldEvidenceLimit)
	for _, tc := range []struct {
		name  string
		build func(string) Report
	}{
		{"pass", func(s string) Report { return Report{Results: []Result{pass("remote", "check", s)}} }},
		{"warn", func(s string) Report { return Report{Results: []Result{warn("remote", "check", s)}} }},
		{"skip", func(s string) Report { return Report{Results: []Result{skip("remote", "check", s)}} }},
		{"remote output", func(s string) Report {
			return Report{Results: []Result{pass("remote", "tool", summarizeRemoteEvidence("tool", s))}}
		}},
		{"cloud-init output", func(s string) Report {
			return Report{Results: []Result{pass("remote", "cloud-init", summarizeRemoteEvidence("cloud-init", s))}}
		}},
		{"remote inventory", func(s string) Report {
			runner := &scriptedRemote{responses: map[string][]remoteCall{remoteInventoryCommand(): {{out: s}}}}
			return Report{Inventory: remoteInventory(context.Background(), runner, "admin", "host")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := tc.build(evidence).Redact(testDiagnosticSecret)
			var output bytes.Buffer
			if err := report.Write(&output); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), testDiagnosticSecret[:testSecretOverlap]) {
				t.Fatal("credential prefix survived evidence truncation")
			}
			var saved Report
			if err := json.Unmarshal(output.Bytes(), &saved); err != nil {
				t.Fatal(err)
			}
			for _, result := range saved.Results {
				if len(result.Evidence) > testOldEvidenceLimit+len("...") {
					t.Fatal("non-failure output lost its size limit")
				}
			}
		})
	}
}
