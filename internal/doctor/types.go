package doctor

import "github.com/assagman/serverpro/internal/compute"

type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

const (
	SudoPasswordCheckName       = "sudo password required"
	SudoPasswordAuthRemediation = "inspect remote sudo password authentication"
)

type Options struct {
	Fix              bool
	SudoPassword     string
	SudoPasswordHash string
	ComputeAccount   compute.Account
}

type InventoryItem struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Result struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	Status      Status `json:"status"`
	Evidence    string `json:"evidence"`
	Remediation string `json:"remediation,omitempty"`
}

func IsSudoPasswordAuthFailure(result Result) bool {
	return result.Name == SudoPasswordCheckName && result.Status == Fail && result.Remediation == SudoPasswordAuthRemediation
}
