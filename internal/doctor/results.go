package doctor

import (
	"strings"
	"unicode/utf8"
)

const (
	maxSummaryEvidenceBytes   = 160
	summaryEvidenceTruncation = "..."
)

func pass(scope, name, evidence string) Result {
	return Result{Name: name, Scope: scope, Status: Pass, Evidence: evidence}
}

func warn(scope, name, evidence string) Result {
	return Result{Name: name, Scope: scope, Status: Warn, Evidence: evidence}
}

func skip(scope, name, evidence string) Result {
	return Result{Name: name, Scope: scope, Status: Skip, Evidence: evidence}
}

func fail(scope, name, evidence, fix string) Result {
	// Failure output must remain intact until credential redaction has run.
	return Result{Name: name, Scope: scope, Status: Fail, Evidence: evidence, Remediation: fix}
}

func trim(s string) string {
	s = strings.ToValidUTF8(s, string(utf8.RuneError))
	if len(s) <= maxSummaryEvidenceBytes {
		return s
	}
	end := maxSummaryEvidenceBytes
	for !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + summaryEvidenceTruncation
}
