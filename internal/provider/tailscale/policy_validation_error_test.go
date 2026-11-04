package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

// The bodies below copy the shapes that the live /acl/validate endpoint sent
// with HTTP 200 on 2026-11-03, with the tailnet names replaced. They guard
// the write gate against a status-only check, which let them all through.
const (
	failedTestsValidationBody = `{"message":"test(s) failed","data":[` +
		`{"user":"alice@example.com","errors":["[ssh test error]: ssh user \"root\": want: \"check\" got: \"deny\""]},` +
		`{"user":"tag:serverpro-prod","errors":["e2","e3","e4","e5","e6"]}]}`
	syntaxValidationBody = `{"message":"SSH tests src contains disallowed element \"autogroup:admin\""}`
	unreadableValidation = `<html>proxy error</html>`
)

func TestEnsureServerproPolicyRefusesWriteWhenValidationReportsFailure(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantText   []string
		wantDecode bool
	}{
		{
			name:     "failed tests",
			body:     failedTestsValidationBody,
			wantText: []string{"test(s) failed", `alice@example.com: [ssh test error]`, "tag:serverpro-prod: e2", "(and 1 more)"},
		},
		{
			name:     "rejected syntax",
			body:     syntaxValidationBody,
			wantText: []string{"disallowed element"},
		},
		{
			name:       "unreadable body",
			body:       unreadableValidation,
			wantText:   []string{errPolicyValidationUnreadable},
			wantDecode: true,
		},
		{
			name:     "null body",
			body:     `null`,
			wantText: []string{errPolicyValidationUnexpected},
		},
		{
			name:     "empty message",
			body:     `{"message":""}`,
			wantText: []string{errPolicyValidationUnexpected},
		},
		{
			name:     "unknown field",
			body:     `{"error":"x"}`,
			wantText: []string{errPolicyValidationUnexpected},
		},
		{
			name:       "empty body",
			body:       "",
			wantText:   []string{errPolicyValidationUnreadable},
			wantDecode: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policyPosts := 0
			handlerErr := testhttp.NewHandlerErrorRecorder(t)
			defer handlerErr.Check()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /tailnet/-/acl":
					w.Header().Set("ETag", `"policy-v1"`)
					_, _ = w.Write([]byte(`{"ssh":[]}`))
				case "POST /tailnet/-/acl/validate":
					_, _ = w.Write([]byte(tc.body))
				case "POST /tailnet/-/acl":
					policyPosts++
				default:
					handlerErr.Record(w, "unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer ts.Close()

			_, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).EnsureServerproPolicy(context.Background(), []string{"tag:serverpro-prod"}, []string{"tag:serverpro-prod"}, "deploy", "check-or-disabled", "")
			var validationErr *PolicyValidationError
			if !errors.As(err, &validationErr) || policyPosts != 0 {
				t.Fatalf("err=%v policyPosts=%d", err, policyPosts)
			}
			for _, want := range tc.wantText {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not contain %q", err, want)
				}
			}
			var syntaxErr *json.SyntaxError
			if tc.wantDecode != (errors.Unwrap(validationErr) != nil) || (tc.wantDecode && !errors.As(err, &syntaxErr)) {
				t.Fatalf("cause = %v", errors.Unwrap(validationErr))
			}
		})
	}
}
