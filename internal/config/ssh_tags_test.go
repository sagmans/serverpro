package config

import (
	"slices"
	"strings"
	"testing"
)

const sshTagsTestOwnTag = "tag:serverpro-prod-webapp"

// sshTagsConfig is a server that carries the namespace tag plus its own tag,
// the shape that separates two servers in one namespace.
func sshTagsConfig(sshTags ...string) Config {
	cfg := Example("prod")
	cfg.Cloudflare.AccountID = "acc"
	cfg.Access.Tailscale.Tags = []string{NamespaceTailscaleTag("prod"), sshTagsTestOwnTag}
	cfg.Access.Tailscale.SSHTags = sshTags
	return cfg
}

// Every policy writer and doctor take the rule destination from this method,
// so an empty ssh_tags must keep the old behavior (all tags) and a set one
// must replace it, never add to it.
func TestSSHRuleTags(t *testing.T) {
	all := sshTagsConfig().Access.Tailscale
	if got := all.SSHRuleTags(); !slices.Equal(got, all.Tags) {
		t.Fatalf("default rule tags = %v, want %v", got, all.Tags)
	}
	own := sshTagsConfig(sshTagsTestOwnTag).Access.Tailscale
	if got := own.SSHRuleTags(); !slices.Equal(got, []string{sshTagsTestOwnTag}) {
		t.Fatalf("narrowed rule tags = %v", got)
	}
}

// A tag the server does not carry would aim the admin rules at other nodes,
// which is the cross-server reach ssh_tags exists to remove, so config load
// must refuse it, and a duplicate that would hide a typo.
func TestValidateSSHTags(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sshTags []string
		wantErr string
	}{
		{name: "own tag", sshTags: []string{sshTagsTestOwnTag}},
		{name: "namespace tag", sshTags: []string{NamespaceTailscaleTag("prod")}},
		{name: "omitted", sshTags: nil},
		{name: "tag of another server", sshTags: []string{"tag:serverpro-prod-buzz"}, wantErr: `entry "tag:serverpro-prod-buzz" must also be in access.tailscale.tags`},
		{name: "automation tag", sshTags: []string{AutomationTailscaleTag}, wantErr: "must also be in access.tailscale.tags"},
		{name: "duplicate", sshTags: []string{sshTagsTestOwnTag, sshTagsTestOwnTag}, wantErr: "more than once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := sshTagsConfig(tc.sshTags...).Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
