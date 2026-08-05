package lifecycle

import (
	"strings"
	"testing"
)

func TestParseGitHubSSHRepoURLDerivesPerRepoNames(t *testing.T) {
	repo, err := parseGitHubSSHRepoURL(" git@github.com:example/example-app.git ")
	if err != nil {
		t.Fatal(err)
	}
	if repo.url != "git@github.com:example/example-app.git" || repo.owner != "example" || repo.name != "example-app" {
		t.Fatalf("bad repo parse: %+v", repo)
	}
	if repo.deployKeyName() != "serverpro_deploy_key_example_example-app" {
		t.Fatalf("bad key name: %q", repo.deployKeyName())
	}
	if repo.hostAlias() != "serverpro-github-example-example-app" {
		t.Fatalf("bad host alias: %q", repo.hostAlias())
	}
	if repo.aliasURL() != "git@serverpro-github-example-example-app:example/example-app.git" {
		t.Fatalf("bad alias URL: %q", repo.aliasURL())
	}
}

func TestNormalizeGitHubSSHRepoURL(t *testing.T) {
	valid := []string{
		" git@github.com:owner/repo.git ",
		"ssh://git@github.com/owner/repo.git",
		"ssh://git@ssh.github.com:443/owner/repo.git",
		"git@github.com:owner/repo",
	}
	for _, repoURL := range valid {
		got, err := NormalizeGitHubSSHRepoURL(repoURL)
		if err != nil {
			t.Fatalf("NormalizeGitHubSSHRepoURL(%q) error = %v", repoURL, err)
		}
		if strings.ContainsAny(got, " \t\n\r") {
			t.Fatalf("NormalizeGitHubSSHRepoURL(%q) kept whitespace: %q", repoURL, got)
		}
	}
	for _, repoURL := range []string{"", "https://github.com/owner/repo", "git@github.com:owner repo.git", "git@github.com:owner/repo/extra.git", "git@github.com:owner/.git"} {
		if _, err := NormalizeGitHubSSHRepoURL(repoURL); err == nil {
			t.Fatalf("NormalizeGitHubSSHRepoURL(%q) unexpectedly succeeded", repoURL)
		}
	}
}
