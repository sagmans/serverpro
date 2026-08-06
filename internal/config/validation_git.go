package config

import "errors"

// validateGit keeps the git section optional and secret-free: identity is
// required whenever a feature needs it, and access stays within the known set.
func (c Config) validateGit() error {
	switch c.Git.Access {
	case "", GitAccessNone, GitAccessDeployKey, GitAccessAccountKey:
	default:
		return errors.New("git.access must be none, deploy-key, or account-key")
	}
	identityMissing := c.Git.Identity.Name == "" || c.Git.Identity.Email == ""
	if c.Git.Access == GitAccessAccountKey && identityMissing {
		return errors.New("git.access account-key requires git.identity name and email")
	}
	if c.Git.Access == GitAccessDeployKey && c.Git.DeployRepository == "" {
		return errors.New("git.access deploy-key requires git.deploy_repository")
	}
	if c.Git.DeployRepository != "" && c.Git.Access != GitAccessDeployKey && c.Git.Access != GitAccessAccountKey {
		return errors.New("git.deploy_repository requires git.access deploy-key or account-key")
	}
	if c.Git.Signing && identityMissing {
		return errors.New("git.signing requires git.identity name and email")
	}
	return nil
}
