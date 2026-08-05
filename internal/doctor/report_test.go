package doctor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReportRedactsEvidence(t *testing.T) {
	report := Report{Results: []Result{{Evidence: "bad secret-token-value", Remediation: "rotate secret-token-value"}}}.Redact("secret-token-value")
	if strings.Contains(report.Results[0].Evidence, "secret-token-value") || strings.Contains(report.Results[0].Remediation, "secret-token-value") {
		t.Fatalf("secret not redacted: %+v", report.Results[0])
	}
}

func TestReportRedactsInventory(t *testing.T) {
	report := Report{Inventory: []InventoryItem{{Scope: "provider", Name: "hetzner", Value: "token secret-token-value"}}}.Redact("secret-token-value")
	if strings.Contains(report.Inventory[0].Value, "secret-token-value") {
		t.Fatalf("secret not redacted from inventory: %+v", report.Inventory[0])
	}
}

func TestReportPassedFailsOnFailure(t *testing.T) {
	if !(Report{Results: []Result{{Status: Pass}, {Status: Warn}, {Status: Skip}}}).Passed() {
		t.Fatal("expected non-failing statuses to pass")
	}
	if (Report{Results: []Result{{Status: Pass}, {Status: Fail}}}).Passed() {
		t.Fatal("expected fail status to fail report")
	}
}

func TestReportWriteFormatsJSON(t *testing.T) {
	report := Report{Inventory: []InventoryItem{{Scope: "provider", Name: "hetzner", Value: "server=1"}}, Results: []Result{{Name: "name", Scope: "scope", Status: Fail, Evidence: "evidence", Remediation: "fix it"}}}
	var jsonOut bytes.Buffer
	if err := report.Write(&jsonOut, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOut.String(), `"inventory"`) || !strings.Contains(jsonOut.String(), `"results"`) || !strings.Contains(jsonOut.String(), `"status": "fail"`) || !strings.Contains(jsonOut.String(), `"remediation": "fix it"`) {
		t.Fatalf("missing JSON report fields: %q", jsonOut.String())
	}
	var decoded struct {
		Inventory []InventoryItem `json:"inventory"`
		Results   []Result        `json:"results"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Inventory) != 1 || len(decoded.Results) != 1 {
		t.Fatalf("decoded report mismatch: %+v", decoded)
	}
}
