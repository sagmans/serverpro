package tailscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateSSHPolicyRejectsBroadNonrootAccept(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ssh":[{"action":"accept","src":["autogroup:member"],"dst":["tag:serverpro-server"],"users":["autogroup:nonroot"]}]}`))
	}))
	defer ts.Close()
	err := NewWithHTTP("token", "-", ts.URL, ts.Client()).ValidateSSHPolicy(context.Background(), []string{"tag:serverpro-server"}, "deploy", "check-or-disabled")
	if err == nil {
		t.Fatal("expected unsafe policy error")
	}
	if strings.Contains(err.Error(), "passwordless sudo") || !strings.Contains(err.Error(), "explicit admin user") {
		t.Fatalf("stale broad nonroot rationale: %v", err)
	}
}

func TestValidateSSHPolicyRejectsUnsafeMatchingRuleAfterAllowedRule(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		user   string
	}{
		{name: "root accept", action: "accept", user: "root"},
		{name: "broad nonroot accept", action: "accept", user: "autogroup:nonroot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := `{"ssh":[` +
				`{"action":"check","src":["group:sre"],"dst":["tag:serverpro-server"],"users":["deploy"]},` +
				`{"action":"` + tc.action + `","src":["autogroup:member"],"dst":["tag:serverpro-server"],"users":["` + tc.user + `"]}` +
				`]}`
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(policy))
			}))
			defer ts.Close()

			err := NewWithHTTP("token", "-", ts.URL, ts.Client()).ValidateSSHPolicy(context.Background(), []string{"tag:serverpro-server"}, "deploy", "check-or-disabled")
			if err == nil || !strings.Contains(err.Error(), "unsafe Tailscale SSH policy") {
				t.Fatalf("ValidateSSHPolicy() error = %v", err)
			}
		})
	}
}

func TestValidateSSHPolicyAcceptsExplicitAdminUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ssh":[{"action":"accept","src":["group:sre"],"dst":["tag:serverpro-server"],"users":["deploy"]}]}`))
	}))
	defer ts.Close()
	if err := NewWithHTTP("token", "-", ts.URL, ts.Client()).ValidateSSHPolicy(context.Background(), []string{"tag:serverpro-server"}, "deploy", "check-or-disabled"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSSHPolicyAcceptsHuJSONPolicy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			// Tailscale policy files are HuJSON, so comments and trailing commas are valid.
			"ssh": [
				{
					"action": "check",
					"src": ["autogroup:member"],
					"dst": ["tag:serverpro-server"],
					"users": ["deploy"],
				},
			],
		}`))
	}))
	defer ts.Close()
	if err := NewWithHTTP("token", "-", ts.URL, ts.Client()).ValidateSSHPolicy(context.Background(), []string{"tag:serverpro-server"}, "deploy", "check-or-disabled"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSSHPolicyRejectsUnsupportedRootPolicy(t *testing.T) {
	err := NewWithHTTP("token", "-", "http://unused", nil).ValidateSSHPolicy(context.Background(), []string{"tag:serverpro-server"}, "deploy", "allow")
	if err == nil || !strings.Contains(err.Error(), "unsupported root policy") {
		t.Fatalf("ValidateSSHPolicy() error = %v", err)
	}
}
