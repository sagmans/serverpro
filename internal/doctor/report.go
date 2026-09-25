package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sagmans/serverpro/internal/redact"
)

const (
	maxFailureEvidenceBytes   = 4 * 1024
	failureEvidenceHeadBytes  = 1024
	failureEvidenceTruncation = "\n... [truncated] ...\n"
)

type Report struct {
	Inventory []InventoryItem `json:"inventory,omitempty"`
	Results   []Result        `json:"results"`
}

func (r Report) Passed() bool {
	for _, x := range r.Results {
		if x.Status == Fail {
			return false
		}
	}
	return true
}

func (r Report) Redact(secrets ...string) Report {
	redactor := redact.New(secrets...)
	for i := range r.Inventory {
		r.Inventory[i].Value = redactor.String(r.Inventory[i].Value)
	}
	for i := range r.Results {
		r.Results[i].Evidence = redactor.String(r.Results[i].Evidence)
		r.Results[i].Remediation = redactor.String(r.Results[i].Remediation)
	}
	return r
}

func (r Report) Write(w io.Writer) error {
	// Bound only the serialized copy, after callers have redacted complete secrets.
	r.Results = slices.Clone(r.Results)
	for i := range r.Results {
		if r.Results[i].Status == Fail {
			r.Results[i].Evidence = trimFailureEvidence(r.Results[i].Evidence)
		}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

func trimFailureEvidence(evidence string) string {
	evidence = strings.ToValidUTF8(evidence, string(utf8.RuneError))
	if len(evidence) <= maxFailureEvidenceBytes {
		return evidence
	}
	// Bootstrap progress can precede the cause by thousands of bytes.
	headEnd := failureEvidenceHeadBytes
	for !utf8.RuneStart(evidence[headEnd]) {
		headEnd--
	}
	tailBytes := maxFailureEvidenceBytes - failureEvidenceHeadBytes - len(failureEvidenceTruncation)
	tailStart := len(evidence) - tailBytes
	for !utf8.RuneStart(evidence[tailStart]) {
		tailStart++
	}
	return evidence[:headEnd] + failureEvidenceTruncation + evidence[tailStart:]
}
