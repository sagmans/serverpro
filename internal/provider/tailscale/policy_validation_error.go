package tailscale

import (
	"fmt"
	"strings"
)

const (
	// errPolicyValidationUnreadable stands in for the API message when the
	// validate body is not JSON, so the error still names what went wrong.
	errPolicyValidationUnreadable = "unreadable validation response"
	// errPolicyValidationUnexpected names a JSON answer that is neither the
	// passing {} nor a message, such as null or an unknown field.
	errPolicyValidationUnexpected = "unexpected validation response"
	// maxPolicyValidationDetails caps the test errors in the error text. A
	// tailnet with many devices repeats one failure per source, and a long
	// list would bury the first cause in logs and terminals.
	maxPolicyValidationDetails = 5
)

// policyValidationResponse is the body of POST /acl/validate. Tailscale sends
// {} for a passing policy, and message plus optional per-source data otherwise.
type policyValidationResponse struct {
	Message string              `json:"message"`
	Data    []PolicyTestFailure `json:"data"`
}

// PolicyTestFailure is one source whose policy tests failed. It is exported
// so callers can show or classify the failing tests without parsing text.
type PolicyTestFailure struct {
	User   string   `json:"user"`
	Errors []string `json:"errors"`
}

// PolicyValidationError means Tailscale rejected a candidate policy, so
// serverpro refused to write it. Callers match it with errors.As.
type PolicyValidationError struct {
	Message  string
	Failures []PolicyTestFailure
	cause    error
}

func (e *PolicyValidationError) Error() string {
	var details []string
	for _, failure := range e.Failures {
		for _, msg := range failure.Errors {
			details = append(details, failure.User+": "+msg)
		}
	}
	text := "tailnet policy validation failed: " + e.Message
	if e.cause != nil {
		text += ": " + e.cause.Error()
	}
	if len(details) == 0 {
		return text
	}
	shown := details[:min(len(details), maxPolicyValidationDetails)]
	text += ": " + strings.Join(shown, "; ")
	if hidden := len(details) - len(shown); hidden > 0 {
		text += fmt.Sprintf(" (and %d more)", hidden)
	}
	return text
}

// Unwrap keeps the JSON decode cause for an unreadable body.
func (e *PolicyValidationError) Unwrap() error {
	return e.cause
}
