package lifecycle

import (
	"fmt"
	"strings"
)

type githubSSHRepo struct {
	url   string
	owner string
	name  string
}

func (r githubSSHRepo) deployKeyName() string {
	return "serverpro_deploy_key_" + r.owner + "_" + r.name
}

func (r githubSSHRepo) hostAlias() string {
	return "serverpro-github-" + r.owner + "-" + r.name
}

func (r githubSSHRepo) aliasURL() string {
	return "git@" + r.hostAlias() + ":" + r.owner + "/" + r.name + ".git"
}

func NormalizeGitHubSSHRepoURL(repoURL string) (string, error) {
	repo, err := parseGitHubSSHRepoURL(repoURL)
	if err != nil {
		return "", err
	}
	return repo.url, nil
}

func parseGitHubSSHRepoURL(repoURL string) (githubSSHRepo, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return githubSSHRepo{}, fmt.Errorf("repository URL required")
	}
	if strings.ContainsAny(repoURL, " \t\n\r") {
		return githubSSHRepo{}, fmt.Errorf("repository URL must not contain whitespace")
	}
	path, ok := strings.CutPrefix(repoURL, "git@github.com:")
	if !ok {
		path, ok = strings.CutPrefix(repoURL, "ssh://git@github.com/")
	}
	if !ok {
		path, ok = strings.CutPrefix(repoURL, "ssh://git@ssh.github.com:443/")
	}
	if !ok {
		return githubSSHRepo{}, fmt.Errorf("repository URL must use GitHub SSH, for example git@github.com:owner/repo.git")
	}
	owner, repoPart, ok := strings.Cut(path, "/")
	if !ok || strings.Contains(repoPart, "/") {
		return githubSSHRepo{}, fmt.Errorf("repository URL must identify exactly one owner and repo")
	}
	repoName := strings.TrimSuffix(repoPart, ".git")
	if !safeGitHubPathComponent(owner) || !safeGitHubPathComponent(repoName) {
		return githubSSHRepo{}, fmt.Errorf("repository URL owner and repo must use only letters, numbers, dot, underscore, or hyphen")
	}
	return githubSSHRepo{url: repoURL, owner: owner, name: repoName}, nil
}

func safeGitHubPathComponent(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
