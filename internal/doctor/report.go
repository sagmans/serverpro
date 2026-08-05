package doctor

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/sagmans/serverpro/internal/redact"
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

func (r Report) Write(w io.Writer, _ bool) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
