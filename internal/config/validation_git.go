package config

import "errors"

// validateGit keeps the git section optional and secret-free: identity is
// required whenever a feature needs it, and access stays within the known set.
func (c Config) validateGit() error {
	switch c.Git.Access {
	case "", "none", "deploy-key", "account-key":
	default:
		return errors.New("git.access must be none, deploy-key, or account-key")
	}
	identityMissing := c.Git.Identity.Name == "" || c.Git.Identity.Email == ""
	if c.Git.Access == "account-key" && identityMissing {
		return errors.New("git.access account-key requires git.identity name and email")
	}
	if c.Git.Signing && identityMissing {
		return errors.New("git.signing requires git.identity name and email")
	}
	return nil
}
