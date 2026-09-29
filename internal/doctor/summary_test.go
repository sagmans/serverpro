package doctor

import (
	"strings"
	"testing"
)

func TestSummaryCountsOrderAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name     string
		statuses []Status
		want     Status
	}{
		{"empty", nil, Pass}, {"skip", []Status{Skip}, Pass}, {"warn", []Status{Pass, Skip, Warn}, Warn}, {"fail", []Status{Fail, Warn, Pass, Skip}, Fail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Report{}
			for _, status := range tc.statuses {
				report.Results = append(report.Results, Result{Name: string(status), Status: status})
			}
			got := report.Summary("namespace", "server", "/report")
			if got.Namespace != "namespace" || got.Server != "server" || got.ReportPath != "/report" || got.Status != tc.want || got.Counts.Total != len(tc.statuses) {
				t.Fatalf("summary = %+v", got)
			}
			var want []Result
			counts := Counts{}
			for _, result := range report.Results {
				counts.Total++
				switch result.Status {
				case Pass:
					counts.Pass++
				case Warn:
					counts.Warn++
				case Fail:
					counts.Fail++
				case Skip:
					counts.Skip++
				}
				if result.Status != Pass {
					want = append(want, result)
				}
			}
			if got.Counts != counts || len(got.Results) != len(want) || got.Results == nil {
				t.Fatalf("counts/results = %+v", got)
			}
			for i := range want {
				if got.Results[i] != want[i] {
					t.Fatalf("result order = %+v", got.Results)
				}
			}
		})
	}
}

func TestSummaryBoundsFailureEvidence(t *testing.T) {
	evidence := strings.Repeat("x", maxFailureEvidenceBytes*2)
	report := Report{Results: []Result{{Status: Fail, Evidence: evidence}}}
	got := report.Summary("namespace", "server", "/report")
	if len(got.Results[0].Evidence) > maxFailureEvidenceBytes || report.Results[0].Evidence != evidence {
		t.Fatal("summary must bound a copy")
	}
}
