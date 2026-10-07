package doctor

import "github.com/sagmans/serverpro/internal/compute"

type Status string

type ResultCode string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

const (
	SudoPasswordCheckName       = "sudo password required"
	SudoPasswordAuthRemediation = "inspect remote sudo password authentication"
	SudoPasswordAuthFailureCode = ResultCode("sudo_password_auth_failure")
	// TailscaleDeviceIdentityCode marks a tailnet node check that found the
	// recorded device missing or changed, or several devices claiming the
	// server's identity. Automation can tell it apart from a plain offline node.
	TailscaleDeviceIdentityCode = ResultCode("tailscale_device_identity")
	// TailscaleDeviceIdentityRemediation covers both causes: stale devices that
	// share the name, and a recorded device that was legitimately replaced and
	// needs state rebound to the new node.
	TailscaleDeviceIdentityRemediation = "confirm which tailnet device is this server; remove stale or unexpected devices with the same name before running commands that reach the host; if the recorded device was replaced on purpose, rebind state with: serverpro server import --force --with-tailscale"
	// remoteChecksBlocked* report that doctor sent nothing to the host because
	// the tailnet device identity check failed.
	remoteChecksBlockedName     = "remote checks"
	remoteChecksBlockedEvidence = "skipped: tailnet device identity is unresolved, so no command or credential was sent to the host"
)

type Options struct {
	Fix              bool
	SudoPassword     string
	SudoPasswordHash string
	ComputeAccount   compute.Account
	// GitHubPAT reaches remote checks so parity can compare the locally
	// stored copy with remote hosts.yml; only fingerprints ever enter evidence.
	GitHubPAT string
}

type InventoryItem struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Result struct {
	Name        string     `json:"name"`
	Scope       string     `json:"scope"`
	Status      Status     `json:"status"`
	Code        ResultCode `json:"code,omitempty"`
	Evidence    string     `json:"evidence"`
	Remediation string     `json:"remediation,omitempty"`
}

func IsSudoPasswordAuthFailure(result Result) bool {
	return result.Status == Fail && result.Code == SudoPasswordAuthFailureCode
}
