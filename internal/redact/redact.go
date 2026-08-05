package redact

import (
	"errors"
	"strings"
)

const Mask = "[REDACTED]"

type Redactor struct{ secrets []string }

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

// Error returns a new error whose message is the redacted form of err.Error().
// A nil err is returned unchanged so callers can write `return r.Error(err)`.
func (r Redactor) Error(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(r.String(err.Error()))
}
