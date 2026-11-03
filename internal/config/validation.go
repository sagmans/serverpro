package config

func (c Config) Validate() error {
	for _, validate := range []func() error{
		c.validateRequired,
		c.validateResourceNames,
		c.validateNamespaceScopes,
		c.validateTailscaleAutomation,
		c.validateMVPConstraints,
		c.validateAdminUsername,
		c.validateGit,
		c.validateReboot,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}
