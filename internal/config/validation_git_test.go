package config

import (
	"strings"
	"testing"
)

func TestValidateGitAccessValues(t *testing.T) {
	for _, access := range []string{"", "none", "deploy-key"} {
		cfg := Example("prod")
		cfg.Git.Access = access
		if err := cfg.Validate(); err != nil {
			t.Fatalf("access %q should validate: %v", access, err)
		}
	}
}

func TestValidateGitRejectsUnknownAccess(t *testing.T) {
	cfg := Example("prod")
	cfg.Git.Access = "everything"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "git.access") {
		t.Fatalf("unknown access should fail validation: %v", err)
	}
}

func TestValidateGitAccountKeyRequiresIdentity(t *testing.T) {
	cfg := Example("prod")
	cfg.Git.Access = "account-key"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "git.identity") {
		t.Fatalf("account-key without identity should fail: %v", err)
	}
	cfg.Git.Identity = GitIdentity{Name: "buzz", Email: "buzz@example.com"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("account-key with identity should validate: %v", err)
	}
}

func TestValidateGitSigningRequiresIdentity(t *testing.T) {
	cfg := Example("prod")
	cfg.Git.Signing = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "git.signing") {
		t.Fatalf("signing without identity should fail: %v", err)
	}
	cfg.Git.Identity = GitIdentity{Name: "buzz", Email: "buzz@example.com"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("signing with identity should validate: %v", err)
	}
}

func TestGitSectionRoundTrips(t *testing.T) {
	cfg := Example("prod")
	cfg.Git = Git{Identity: GitIdentity{Name: "buzz", Email: "buzz@example.com"}, Signing: true, Access: "account-key"}
	path := writeConfigFixture(t, "namespace: prod\n")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Git != cfg.Git {
		t.Fatalf("git section lost in round trip: %+v", loaded.Git)
	}
}

func TestOmittedGitSectionStaysZero(t *testing.T) {
	cfg := Example("prod")
	path := writeConfigFixture(t, "namespace: prod\n")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Git != (Git{}) {
		t.Fatalf("omitted git section should stay zero: %+v", loaded.Git)
	}
}
