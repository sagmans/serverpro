package tailscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/mesh"
)

const (
	sshTagsTestNamespaceTag = "tag:serverpro-prod"
	sshTagsTestOwnTag       = "tag:serverpro-prod-webapp"
)

// A server that narrows its rules still needs an owner for every tag it
// enrols with, but its check and accept rules must name only its own tag:
// a rule on the shared namespace tag would reach every server in it.
func TestEnsureServerproPolicyNarrowsRulesToSSHTags(t *testing.T) {
	api, client := newAutomationPolicyAPI(t, `{"ssh":[]}`)
	tags := []string{sshTagsTestNamespaceTag, sshTagsTestOwnTag}

	change, err := client.EnsureServerproPolicy(context.Background(), tags, []string{sshTagsTestOwnTag}, automationTestAdmin, automationTestRoot, mesh.AutomationSourceTag, "check")
	if err != nil {
		t.Fatal(err)
	}
	if owners := slices.Sorted(slices.Values(change.TagOwners)); !slices.Equal(owners, []string{mesh.AutomationSourceTag, sshTagsTestNamespaceTag, sshTagsTestOwnTag}) {
		t.Fatalf("tag owners = %v", change.TagOwners)
	}
	rules := api.postedSSH(t)
	if len(rules) != 2 {
		t.Fatalf("posted ssh rules = %+v", rules)
	}
	for _, rule := range rules {
		if !slices.Equal(rule.Dst, []string{sshTagsTestOwnTag}) {
			t.Fatalf("rule %+v reaches more than the own tag", rule)
		}
	}
}

// The read-back checks the accept rule for sshTags, but scans every device
// tag for unsafe rules: a root accept rule on the namespace tag reaches the
// server even when its own rules are narrowed.
func TestValidateSSHPolicyScansEveryDeviceTag(t *testing.T) {
	own := `{"action":"check","src":["autogroup:admin"],"dst":["` + sshTagsTestOwnTag + `"],"users":["deploy"]},` +
		`{"action":"accept","src":["` + mesh.AutomationSourceTag + `"],"dst":["` + sshTagsTestOwnTag + `"],"users":["deploy"]}`
	tags := []string{sshTagsTestNamespaceTag, sshTagsTestOwnTag}
	for _, tc := range []struct {
		name    string
		rules   string
		wantErr string
	}{
		{name: "own rules only", rules: own},
		{name: "root accept on namespace tag", rules: own + `,{"action":"accept","src":["autogroup:member"],"dst":["` + sshTagsTestNamespaceTag + `"],"users":["root"]}`, wantErr: "root access"},
		{name: "accept rule only on namespace tag", rules: `{"action":"accept","src":["` + mesh.AutomationSourceTag + `"],"dst":["` + sshTagsTestNamespaceTag + `"],"users":["deploy"]}`, wantErr: "has no accept rule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"ssh":[` + tc.rules + `]}`))
			}))
			defer srv.Close()
			err := NewWithHTTP("token", "-", srv.URL, srv.Client()).ValidateSSHPolicy(context.Background(), tags, []string{sshTagsTestOwnTag}, "deploy", automationTestRoot, mesh.AutomationSourceTag, "check")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateSSHPolicy() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
