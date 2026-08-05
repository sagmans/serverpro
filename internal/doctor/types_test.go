package doctor

import "testing"

// WHY: presentation text changes independently from CLI control flow. Pin the
// stable result code so copy edits cannot silently break sudo re-entry.
func TestIsSudoPasswordAuthFailureMatchesTypedCode(t *testing.T) {
	result := Result{
		Name:        "renamed check",
		Status:      Fail,
		Code:        SudoPasswordAuthFailureCode,
		Remediation: "rewritten remediation",
	}
	if !IsSudoPasswordAuthFailure(result) {
		t.Fatal("expected sudo password auth failure to be detected")
	}
}

func TestIsSudoPasswordAuthFailureRejectsNonMatches(t *testing.T) {
	cases := map[string]Result{
		"passing status": {Status: Pass, Code: SudoPasswordAuthFailureCode},
		"other code":     {Status: Fail, Code: "other_failure"},
		"empty code":     {Status: Fail},
	}
	for name, result := range cases {
		if IsSudoPasswordAuthFailure(result) {
			t.Fatalf("%s should not be classified as auth failure", name)
		}
	}
}
