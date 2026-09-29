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

// RedactForOutput preserves internal check identity through retries, then masks
// metadata only on the final bounded copy used by logs and terminal output.
func (r Report) RedactForOutput(secrets ...string) Report {
	r.Results = slices.Clone(r.Results)
	r.Inventory = slices.Clone(r.Inventory)
	r = r.Redact(secrets...).boundedEvidence()
	redactor := redact.New(secrets...)
	for i := range r.Inventory {
		r.Inventory[i].Scope = redactor.String(r.Inventory[i].Scope)
		r.Inventory[i].Name = redactor.String(r.Inventory[i].Name)
	}
	for i := range r.Results {
		r.Results[i].Scope = redactor.String(r.Results[i].Scope)
		r.Results[i].Name = redactor.String(r.Results[i].Name)
		r.Results[i].Code = ResultCode(redactor.String(string(r.Results[i].Code)))
	}
	return r
}

// boundedEvidence keeps credential fragments out of both output formats by
// applying limits only after the CLI has redacted the complete report.
func (r Report) boundedEvidence() Report {
	r.Results = slices.Clone(r.Results)
	for i := range r.Results {
		if r.Results[i].Status == Fail {
			r.Results[i].Evidence = trimFailureEvidence(r.Results[i].Evidence)
		} else {
			r.Results[i].Evidence = trim(r.Results[i].Evidence)
		}
	}
	r.Inventory = slices.Clone(r.Inventory)
	for i := range r.Inventory {
		if r.Inventory[i].Scope == remoteInventoryScope && r.Inventory[i].Name == remoteInventoryName {
			r.Inventory[i].Value = trim(r.Inventory[i].Value)
		}
	}
	return r
}

func (r Report) Write(w io.Writer) error {
	r = r.boundedEvidence()
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
