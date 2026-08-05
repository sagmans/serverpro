package tailscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInspectServerproPolicyPartsReportsExactPresence(t *testing.T) {
	tests := []struct {
		name     string
		policy   string
		wantTags int
		wantSSH  bool
	}{
		{name: "present", policy: `{"tagOwners":{"tag:serverpro-prod":["autogroup:admin"]},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["deploy"]}]}`, wantTags: 1, wantSSH: true},
		{name: "absent", policy: `{"tagOwners":{},"ssh":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/tailnet/-/acl" {
					t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(test.policy))
			}))
			defer ts.Close()

			present, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).InspectServerproPolicyParts(context.Background(), []string{"tag:serverpro-prod"}, []string{"tag:serverpro-prod"}, "deploy")
			if err != nil {
				t.Fatal(err)
			}
			if len(present.TagOwners) != test.wantTags || present.SSHRule != test.wantSSH {
				t.Fatalf("presence = %+v", present)
			}
		})
	}
}

func TestInspectServerproPolicyPartsRejectsDrift(t *testing.T) {
	tests := []struct {
		name   string
		policy string
	}{
		{name: "tag owner", policy: `{"tagOwners":{"tag:serverpro-prod":["group:ops"]},"ssh":[]}`},
		{name: "ssh rule", policy: `{"tagOwners":{},"ssh":[{"action":"check","src":["autogroup:admin"],"dst":["tag:serverpro-prod"],"users":["root"]}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.policy))
			}))
			defer ts.Close()

			_, err := NewWithHTTP("token", "-", ts.URL, ts.Client()).InspectServerproPolicyParts(context.Background(), []string{"tag:serverpro-prod"}, []string{"tag:serverpro-prod"}, "deploy")
			if err == nil || !strings.Contains(err.Error(), "ownership drift") {
				t.Fatalf("expected ownership drift, got %v", err)
			}
		})
	}
}
