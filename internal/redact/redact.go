package redact

import "strings"

const Mask = "[REDACTED]"

type Redactor struct{ secrets []string }

type redactedError struct {
	message string
	cause   error
}

func (e redactedError) Error() string { return e.message }

func (e redactedError) Unwrap() error { return e.cause }

func New(secrets ...string) Redactor {
	var keep []string
	seen := map[string]bool{}
	for _, s := range secrets {
		if len(s) < 4 || seen[s] {
			continue
		}
		seen[s] = true
		keep = append(keep, s)
	}
	return Redactor{secrets: keep}
}

func (r Redactor) String(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Mask)
	}
	return s
}

// Error returns an error whose public message is redacted while Unwrap keeps
// sentinel and typed identity available for cancellation and retry decisions.
// A nil err is returned unchanged so callers can write `return r.Error(err)`.
func (r Redactor) Error(err error) error {
	if err == nil {
		return nil
	}
	return redactedError{message: r.String(err.Error()), cause: err}
}
